// Package genapi implements a generic skeleton we can use as the basis for an
// api service. It will set up command line arguments, connections to backend
// databases, handle test modes which might affect those databases, register
// itself with skyapi, and more.
//
// Basic definition
//
// To use first initialize a GenAPI instance somewhere accessible by your entire
// application, and give it an RPC type
//
//	package myapi
//
//	var GA = genapi.GenAPI{
//		Name:      "my-api",
//		RedisInfo: &genapi.RedisInfo{}
//		Services:  []interface{}{MyAPI{}},
//	}
//
//	type MyAPI struct{}
//
//	func (_ MyAPI) Foo(r *http.Request, args *struct{}, res *struct{}) error {
//		return GA.Cmd("INCR", "MyKey").Err
//	}
//
// API Mode
//
// To actually read command-line arguments, set up database connections, listen
// on a random port, register with skyapi, and start handling requests, simply
// call APIMode() from your main method:
//
//	package main
//
//	func main() {
//		myapi.GA.APIMode()
//	}
//
// In APIMode the genapi will also listen for SIGTERM signals, and if it
// receives one will unregister with skyapi, and exit once all ongoing requests
// are completed.
//
// Test Mode
//
// When testing your api you can call TestMode from your test's init function,
// and call RPC to get an instance of an http.Handler you can make calls
// against:
//
//	package myapi // myapi_test.go
//
//	import . "testing"
//
//	func init() {
//		GA.TestMode()
//	}
//
//	func TestSomeThing(t *T) {
//		h := GA.RPC()
//		// test against h
//	}
//
// CLI Mode
//
// Finally, there are times when you want a command-line binary which will be
// made alongside the actual api binary, and which will share resources and
// possibly database connections. In this case you can use the CLIMode method
// and then access the GenAPI from your main method as normal:
//
//	package main
//
//	func main() {
//		myapi.GA.CLIMode()
//		myapi.GA.Cmd("DECR", "MyKey")
//	}
//
package genapi

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gorilla/rpc/v2"
	"github.com/levenlabs/gatewayrpc"
	"github.com/levenlabs/go-llog"
	"github.com/levenlabs/go-srvclient"
	"github.com/levenlabs/golib/mgoutil"
	"github.com/levenlabs/golib/radixutil"
	"github.com/levenlabs/golib/rpcutil"
	"github.com/mediocregopher/lever"
	"github.com/mediocregopher/radix.v2/util"
	"github.com/mediocregopher/skyapi/client"
	"gopkg.in/mgo.v2"
)

// This can be set using:
//	-ldflags "-X github.com/levenlabs/golib/genapi.Version versionstring"
// on the go build command. When this is done, the --version flag will be
// available on the command-line and will print out whatever version string is
// passed in.
//
// It could also be set manually during runtime, but that would kind of defeat
// the purpose.
//
// Version will be automatically unquoted
var Version string

// MongoInfo contains information needed by the api to interact with a mongo
// backend, and also houses the connection to that backend (which can be
// interacted with through its methods)
type MongoInfo struct {
	// The name of the mongo database this app should use. In TestMode this will
	// always be overwritten to "test_<DBName>"
	DBName string

	session *mgo.Session
}

// WithDB is similar to mgoutil.SessionHelper's WithDB, see those docs for more
// details
func (m *MongoInfo) WithDB(fn func(*mgo.Database)) {
	mgoutil.SessionHelper{
		Session: m.session,
		DB:      m.DBName,
	}.WithDB(fn)
}

// WithColl is similar to mgoutil.SessionHelper's WithColl, see those docs for
// more details
func (m *MongoInfo) WithColl(collName string, fn func(*mgo.Collection)) {
	mgoutil.SessionHelper{
		Session: m.session,
		DB:      m.DBName,
		Coll:    collName,
	}.WithColl(fn)
}

// CollSH returns an mgoutil.SessionHelper for a collection of the given name
func (m *MongoInfo) CollSH(collName string) mgoutil.SessionHelper {
	return mgoutil.SessionHelper{
		Session: m.session,
		DB:      m.DBName,
		Coll:    collName,
	}
}

// RedisInfo is used to tell the api to interact with a redis backend, and also
// houses the connection to that backend. If the redis backend is a cluster
// instance that whole cluster will be connected to
type RedisInfo struct {
	// Populated by the api once a connection to redis is made, and can be used
	// as such. Do not set manually.
	util.Cmder
}

// GenAPI is a type used to handle most of the generic logic we always implement
// when making an RPC API endpoint.
//
// The struct is initialized with whatever parameters are appropriate, and then
// has either APIMode(), TestMode(), or CLIMode() called on it depending on the
// intent. Fields are optional unless otherwise marked in the comment.
type GenAPI struct {
	// Required. Name is the name of the api, as it will be identified on the
	// command-line and in skyapi
	Name string

	// Required. The set of rpc service structs which this API will host. Must
	// have at least one service in APIMode
	Services []interface{}

	// Like Services, but these will not be registered with the underlying
	// gateway library, and therefore will not show up in calls to
	// "RPC.GetMethods"
	HiddenServices []interface{}

	// Additional lever.Param structs which can be included in the lever parsing
	LeverParams []lever.Param

	// If mongo is intended to be used as a backend, this should be filled in
	*MongoInfo

	// If redis is intended to be used, this should be filled in.
	*RedisInfo

	// A function to run just after initializing connections to backing
	// database. Meant for performing any initialization needed by the app.
	Init func(*GenAPI)

	// May be set if a codec with different parameters is required.
	// If not set an rpcutil.LLCodec with default options will be used.
	Codec rpc.Codec

	// Do not set. This will be automatically filled in with whatever address
	// is being listened on once APIMode is called.
	ListenAddr string

	// Do not set. This will be automatically filled in when any of the run
	// modes are called, and may be used after that point to retrieve parameter
	// values.
	*lever.Lever

	// Do not set. This will be automatically filled in when any of the run
	// modes are called. Indicates which mode the GenAPI is currently in, and
	// may be used after that point to know which run mode GenAPI is in.
	Mode string

	// When initialized, this channel will be closed at the end of the init
	// phase of running. If in APIMode it will be closed just before the call to
	// ListenAndServe. This is useful so you can call APIMode in a separate
	// go-routine and know when it's started listening, if there's other steps
	// you want to take after initialization has been done.
	InitDoneCh chan bool
}

// The different possible Mode values for GenAPI
const (
	APIMode  = "api"
	TestMode = "test"
	CLIMode  = "cli"
)

// APIMode puts the GenAPI into APIMode, wherein it listens for any incoming rpc
// requests and tries to serve them against one of its Services. This method
// will block indefinitely
func (g *GenAPI) APIMode() {
	g.Mode = APIMode
	g.init()
	h := g.RPC()

	g.ListenAddr, _ = g.Lever.ParamStr("--listen-addr")

	// We listen on a random port if none is given. We have to jump through
	// some hoops to actually do this, most of these hoops being copied from
	// what ListenAndServe is doing in net/http

	kv := llog.KV{"addr": g.ListenAddr}
	llog.Info("creating listen socket", kv)
	ln, err := net.Listen("tcp", g.ListenAddr)
	if err != nil {
		kv["err"] = err
		llog.Fatal("failed creating listen socket", kv)
	}
	g.ListenAddr = ln.Addr().String()
	kv["addr"] = g.ListenAddr

	// Once ListenAddr is populated with the final value we can call doSkyAPI
	skyapiStopCh := g.doSkyAPI()

	hw := &httpWaiter{
		ch: make(chan struct{}, 1),
	}

	srv := &http.Server{
		Addr:    g.ListenAddr,
		Handler: hw.handler(h),
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		llog.Info("starting rpc listening", kv)
		srv.Serve(tcpKeepAliveListener{ln.(*net.TCPListener)})
	}()

	<-sigCh
	llog.Info("signal received, stopping")
	if skyapiStopCh != nil {
		llog.Info("stopping skyapi connection")
		close(skyapiStopCh)
		// Wait a bit just in case something gets the skydns record before we
		// kill the skyapi connection, but the connection doesn't come in till
		// after hw.wait() runs
		time.Sleep(500 * time.Millisecond)
	}
	hw.wait()
	time.Sleep(50 * time.Millisecond)
}

// TestMode puts the GenAPI into TestMode, wherein it is then prepared to be
// used for during go tests
func (g *GenAPI) TestMode() {
	g.Mode = TestMode
	g.init()
}

// CLIMode puts the GenAPI into CLIMode, wherein it is then prepared to be used
// by a command-line utility
func (g *GenAPI) CLIMode() {
	g.Mode = CLIMode
	g.init()
}

// RPC returns an http.Handler which will handle the RPC calls made against it
// for the GenAPI's Services
func (g *GenAPI) RPC() http.Handler {
	s := gatewayrpc.NewServer()
	s.RegisterCodec(g.Codec, "application/json")
	for _, service := range g.Services {
		if err := s.RegisterService(service, ""); err != nil {
			llog.Fatal("error registering service", llog.KV{
				"service": fmt.Sprintf("%T", service),
				"err":     err,
			})
		}
	}
	for _, service := range g.HiddenServices {
		if err := s.RegisterHiddenService(service, ""); err != nil {
			llog.Fatal("error registering hidden service", llog.KV{
				"service": fmt.Sprintf("%T", service),
				"err":     err,
			})
		}
	}
	return s
}

func (g *GenAPI) init() {
	rpcutil.InstallCustomValidators()
	g.doLever()

	if g.Lever.ParamFlag("--version") {
		v := Version
		if v[0] != '"' {
			v = `"` + v + `"`
		}
		if uv, err := strconv.Unquote(v); err == nil {
			v = uv
		}
		fmt.Println(v)
		time.Sleep(100 * time.Millisecond)
		os.Exit(0)
	}

	ll, _ := g.ParamStr("--log-level")
	llog.SetLevelFromString(ll)

	llog.Info("starting GenAPI", llog.KV{"mode": g.Mode, "name": g.Name})
	if g.MongoInfo != nil {
		g.initMongo()
	}

	if g.RedisInfo != nil {
		g.initRedis()
	}

	if g.Codec == nil {
		c := rpcutil.NewLLCodec()
		c.ValidateInput = true
		g.Codec = c
	}

	if g.Init != nil {
		g.Init(g)
	}

	if g.InitDoneCh != nil {
		close(g.InitDoneCh)
	}
}

func (g *GenAPI) doLever() {
	o := &lever.Opts{}
	if g.Mode == CLIMode {
		o.DisallowConfigFile = true
	}
	g.Lever = lever.New(g.Name, o)

	if g.Mode == APIMode {
		g.Lever.Add(lever.Param{
			Name:        "--listen-addr",
			Description: "[address]:port to listen for rpc requests on. If port is zero a port will be chosen randomly",
			Default:     ":0",
		})
		g.Lever.Add(lever.Param{
			Name:        "--skyapi-addr",
			Description: "Hostname of skyapi, to be looked up via a SRV request. Unset means don't register with skyapi",
		})
	}

	if g.MongoInfo != nil {
		g.Lever.Add(lever.Param{
			Name:        "--mongo-addr",
			Description: "Address of mongo instance to use",
			Default:     "127.0.0.1:27017",
		})
	}

	if g.RedisInfo != nil {
		g.Lever.Add(lever.Param{
			Name:        "--redis-addr",
			Description: "Address of redis instance to use. May be a single member of a cluster",
			Default:     "127.0.0.1:6379",
		})
		g.Lever.Add(lever.Param{
			Name:        "--redis-pool-size",
			Description: "Number of connections to a single redis instance to use. If a cluster is being used, this many connections will be made to each member of the cluster",
			Default:     "10",
		})
	}

	if Version != "" {
		g.Lever.Add(lever.Param{
			Name:        "--version",
			Aliases:     []string{"-v"},
			Description: "Print out version information for this binary",
			Flag:        true,
		})
	}

	for _, p := range g.LeverParams {
		g.Lever.Add(p)
	}

	g.Lever.Parse()
}

func (g *GenAPI) doSkyAPI() chan struct{} {
	skyapiAddr, _ := g.Lever.ParamStr("--skyapi-addr")
	if skyapiAddr == "" {
		return nil
	}

	skyapiAddr = srvclient.MaybeSRV(skyapiAddr)
	kv := llog.KV{"skyapiAddr": skyapiAddr, "listenAddr": g.ListenAddr}
	llog.Info("connecting to skyapi", kv)
	stopCh := make(chan struct{})
	go func() {
		kv["err"] = client.ProvideOpts(client.Opts{
			SkyAPIAddr:        skyapiAddr,
			Service:           g.Name,
			ThisAddr:          g.ListenAddr,
			ReconnectAttempts: -1,
			StopCh:            stopCh,
		})
		llog.Fatal("skyapi giving up reconnecting", kv)
	}()
	return stopCh
}

func (g *GenAPI) initMongo() {
	if g.Mode == TestMode {
		g.MongoInfo.DBName = "test_" + g.MongoInfo.DBName
	}

	mongoAddr, _ := g.ParamStr("--mongo-addr")
	g.MongoInfo.session = mgoutil.EnsureSession(mongoAddr)
}

func (g *GenAPI) initRedis() {
	redisAddr, _ := g.ParamStr("--redis-addr")
	redisPoolSize, _ := g.ParamInt("--redis-pool-size")
	var err error
	g.RedisInfo.Cmder, err = radixutil.DialMaybeCluster("tcp", redisAddr, redisPoolSize)

	if err != nil {
		llog.Fatal("error connecting to redis", llog.KV{
			"addr":     redisAddr,
			"poolSize": redisPoolSize,
		})
	}
}
