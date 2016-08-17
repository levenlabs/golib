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

/*
import (
	"crypto/sha1"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/context"

	"github.com/levenlabs/go-llog"
	"github.com/levenlabs/go-srvclient"
	"github.com/levenlabs/golib/mgoutil"
	"github.com/levenlabs/golib/radixutil"
	"github.com/levenlabs/golib/rpcutil"
	"github.com/mediocregopher/lever"
	"github.com/mediocregopher/okq-go.v2"
	"github.com/mediocregopher/radix.v2/pool"
	"github.com/mediocregopher/radix.v2/util"
	"github.com/mediocregopher/skyapi/client"
	"github.com/miekg/dns"
	"gopkg.in/mgo.v2"
)

// Version can be set using:
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

	// Additional lever.Param structs which can be included in the lever parsing
	LeverParams []lever.Param

	// If mongo is intended to be used as a backend, this should be filled in
	*MongoInfo

	// If redis is intended to be used, this should be filled in.
	*RedisInfo

	// If okq is intended to be used, this should be filled in.
	*OkqInfo

	// A function to run just after initializing connections to backing
	// database. Meant for performing any initialization needed by the app.
	// This is called before any AppendInit functions
	Init InitFunc

	inits []InitFunc

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

	// When initialized, this channel will be closed when in APIMode and cleanup
	// has been completed after a kill signal. This is useful if you have other
	// cleanup you want to run after GenAPI is done.
	DoneCh chan bool

	// Optional set of remote APIs (presumably GenAPIs, but that's not actually
	// required) that this one will be calling. The key should be the name of
	// the remote api, and the value should be the default address for it. Each
	// one will have a configuration option added for its address (e.g. if
	// "other-api" is in this list, then "--other-api-addr" will be a config
	// option). Each key can be used as an argument to RemoteAPICaller to obtain
	// a convenient function for communicating with other apis.
	RemoteAPIs map[string]string

	// SRVClient which will be used by GenAPI when resolving requests, and which
	// can also be used by other processes as well. This should only be modified
	// during the init function
	srvclient.SRVClient

	// set of active listeners for this genapi (APIMode only)
	listeners []*listenerReloader
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
	g.RPCListen()

	// Once ListenAddr is populated with the final value we can call doSkyAPI
	skyapiStopCh := g.doSkyAPI()

	if g.InitDoneCh != nil {
		close(g.InitDoneCh)
	}

	// After this point everything is listening and we're just waiting for a
	// kill signal

	llog.Info("waiting for close signal")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
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
	g.hw.wait() // hw is populated in RPCListen
	time.Sleep(50 * time.Millisecond)

	if g.DoneCh != nil {
		close(g.DoneCh)
	}

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

// AppendInit adds a function to be called when GenAPI is initialized. It will
// be called after GenAPI's Init() and after any previous functions that were
// appended
func (g *GenAPI) AppendInit(f InitFunc) {
	if g.Mode != "" {
		panic("genapi: AppendInit was called after Init has already been ran")
	}
	g.inits = append(g.inits, f)
}

func (g *GenAPI) init() {
	g.ctxs = map[*http.Request]context.Context{}
	rpcutil.InstallCustomValidators()
	g.SRVClient.EnableCacheLast()
	g.doLever()

	g.SRVClient.Preprocess = g.srvClientPreprocess

	if g.RPCEndpoint == "" {
		g.RPCEndpoint = "/"
	}

	if g.Mux == nil {
		g.Mux = http.NewServeMux()
	}

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

	if g.OkqInfo != nil {
		g.initOkq()
	}

	if g.Codec == nil {
		c := rpcutil.NewLLCodec()
		c.ValidateInput = true
		c.RunInputApplicators = true
		g.Codec = c
	}

	tlsAddrs, _ := g.ParamStrs("--tls-listen-addr")
	if g.TLSInfo != nil && !g.TLSInfo.FillCertsManually && len(tlsAddrs) > 0 {
		certFiles, _ := g.ParamStrs("--tls-cert-file")
		keyFiles, _ := g.ParamStrs("--tls-key-file")
		if len(certFiles) == 0 {
			llog.Fatal("no --tls-cert-file provided")
		}
		if len(certFiles) != len(keyFiles) {
			llog.Fatal("number of --tls-cert-file must match number of --tls-key-file")
		}
		for i := range certFiles {
			kv := llog.KV{"certFile": certFiles[i], "keyFile": keyFiles[i]}
			llog.Info("loading tls cert", kv)
			c, err := tls.LoadX509KeyPair(certFiles[i], keyFiles[i])
			if err != nil {
				llog.Fatal("failed to load tls cert", kv, llog.KV{"err": err})
			}
			g.TLSInfo.Certs = append(g.TLSInfo.Certs, c)
		}
	}

	g.countCh = make(chan bool)
	go func() {
		t := time.Tick(1 * time.Minute)
		var c uint64
		for {
			select {
			case <-g.countCh:
				c++
			case <-t:
				llog.Info("count requests in last minute", llog.KV{"count": c})
				c = 0
			}
		}
	}()

	if g.Init != nil {
		// make sure the struct's Init is always called first
		g.Init(g)
	}
	for _, f := range g.inits {
		f(g)
	}

	// InitDoneCh gets closed at the end of APIMode being called
	if g.Mode != APIMode && g.InitDoneCh != nil {
		close(g.InitDoneCh)
	}
}

func (g *GenAPI) doLever() {
	o := &lever.Opts{}
	if g.Mode == CLIMode {
		o.DisallowConfigFile = true
	}
	g.Lever = lever.New(g.Name, o)
	g.Lever.Add(lever.Param{
		Name:        "--log-level",
		Description: "Log level to run with. Available levels are: debug, info, warn, error, fatal",
		Default:     "info",
	})

	g.Lever.Add(lever.Param{
		Name:        "--datacenter",
		Description: "What datacenter the service is running in",
		Default:     os.Getenv("DATACENTER"),
	})

	// The listen-addr parameters can be used outside of APIMode through the
	// RPCListener method
	g.Lever.Add(lever.Param{
		Name:         "--listen-addr",
		Description:  "[address]:port to listen for requests on. If port is zero a port will be chosen randomly",
		DefaultMulti: []string{":0"},
	})
	if g.TLSInfo != nil {
		g.Lever.Add(lever.Param{
			Name:         "--tls-listen-addr",
			Description:  "[address]:port to listen for https requests on. If port is zero a port will be chosen randomly",
			DefaultMulti: []string{},
		})
		if !g.TLSInfo.FillCertsManually {
			g.Lever.Add(lever.Param{
				Name:        "--tls-cert-file",
				Description: "Certificate file to use for TLS. Maybe be specified more than once. Must be specified as many times as --tls-key-file.",
			})
			g.Lever.Add(lever.Param{
				Name:        "--tls-key-file",
				Description: "Key file to use for TLS. Maybe be specified more than once. Must be specified as many times as --tls-cert-file.",
			})
		}
	}

	if g.Mode == APIMode {
		g.Lever.Add(lever.Param{
			Name:        "--skyapi-addr",
			Description: "Hostname of skyapi, to be looked up via a SRV request. Unset means don't register with skyapi",
		})
		g.Lever.Add(lever.Param{
			Name:        "--proxy-proto-allowed-cidrs",
			Description: "Comma separated list of cidrs which are allowed to use the PROXY protocol",
			Default:     "127.0.0.1/32,::1/128,10.0.0.0/8",
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

	if g.OkqInfo != nil {
		g.Lever.Add(lever.Param{
			Name:        "--okq-addr",
			Description: "Address of okq instance to use",
			Default:     "127.0.0.1:4777",
		})
		g.Lever.Add(lever.Param{
			Name:        "--okq-pool-size",
			Description: "Number of connections to okq to initially make",
			Default:     "10",
		})
	}

	if Version != "" {
		g.Lever.Add(lever.Param{
			Name:        "--version",
			Aliases:     []string{"-V"},
			Description: "Print out version information for this binary",
			Flag:        true,
		})
	}

	for rapi, raddr := range g.RemoteAPIs {
		g.Lever.Add(lever.Param{
			Name:        "--" + rapi + "-addr",
			Description: "Address or hostname of a " + rapi + " instance",
			Default:     raddr,
		})
	}

	for _, p := range g.LeverParams {
		g.Lever.Add(p)
	}

	g.Lever.Parse()
}

func (g *GenAPI) srvClientPreprocess(m *dns.Msg) {
	dc := g.getDCHash()
	if dc == "" {
		return
	}
	for i := range m.Answer {
		if ansSRV, ok := m.Answer[i].(*dns.SRV); ok {
			tar := ansSRV.Target
			if strings.HasPrefix(tar, dc+"-") {
				if ansSRV.Priority < 2 {
					ansSRV.Priority = uint16(0)
				} else {
					ansSRV.Priority = ansSRV.Priority - 1
				}
			}
		}
	}
}

func (g *GenAPI) getDCHash() string {
	dc, _ := g.Lever.ParamStr("--datacenter")
	if dc == "" {
		return ""
	}
	sha1Bytes := sha1.Sum([]byte(dc))
	return fmt.Sprintf("%x", sha1Bytes)[:20]
}

func (g *GenAPI) doSkyAPI() chan struct{} {
	skyapiAddr, _ := g.Lever.ParamStr("--skyapi-addr")
	if skyapiAddr == "" {
		return nil
	}
	dc := g.getDCHash()

	kv := llog.KV{
		"skyapiAddr":  skyapiAddr,
		"listenAddr":  g.ListenAddr,
		"serviceName": g.Name,
		"prefix":      dc,
	}
	stopCh := make(chan struct{})
	go func() {
		for {
			llog.Info("connecting to skyapi", kv)
			err := client.ProvideOpts(client.Opts{
				SkyAPIAddr:        skyapiAddr,
				Service:           g.Name,
				ThisAddr:          g.ListenAddr,
				ReconnectAttempts: 0, // do not attempt to reconnect, we'll do that here
				StopCh:            stopCh,
				Prefix:            dc,
			})
			if err != nil {
				llog.Error("skyapi error", kv.Set("err", err))
				time.Sleep(1 * time.Second)
			} else {
				// If there wasn't an error but skyapi stopped, it's because the
				// stopCh was closed
				return
			}
		}
	}()
	return stopCh
}

func (g *GenAPI) initMongo() {
	if g.Mode == TestMode {
		g.MongoInfo.DBName = "test_" + g.MongoInfo.DBName
	}

	mongoAddr, _ := g.ParamStr("--mongo-addr")
	if mongoAddr == "" && g.MongoInfo.Optional {
		return
	}
	g.MongoInfo.session = mgoutil.EnsureSession(mongoAddr)
}

func (g *GenAPI) initRedis() {
	redisAddr, _ := g.ParamStr("--redis-addr")
	if redisAddr == "" && g.RedisInfo.Optional {
		return
	}
	redisPoolSize, _ := g.ParamInt("--redis-pool-size")
	kv := llog.KV{
		"addr":     redisAddr,
		"poolSize": redisPoolSize,
	}

	llog.Info("connecting to redis", kv)
	var err error
	g.RedisInfo.Cmder, err = radixutil.DialMaybeCluster("tcp", redisAddr, redisPoolSize)

	if err != nil {
		llog.Fatal("error connecting to redis", kv, llog.KV{"err": err})
	}
}

func (g *GenAPI) initOkq() {
	okqAddr, _ := g.ParamStr("--okq-addr")
	if okqAddr == "" && g.OkqInfo.Optional {
		return
	}
	okqPoolSize, _ := g.ParamInt("--okq-pool-size")
	kv := llog.KV{
		"addr":     okqAddr,
		"poolSize": okqPoolSize,
	}

	if g.OkqInfo.Timeout == 0 {
		g.OkqInfo.Timeout = 30 * time.Second
	}
	df := radixutil.SRVDialFunc(g.SRVClient, g.OkqInfo.Timeout)

	llog.Info("connecting to okq", kv)
	p, err := pool.NewCustom("tcp", okqAddr, okqPoolSize, df)
	if err != nil {
		llog.Fatal("error connection to okq", kv, llog.KV{"err": err})
	}

	g.OkqInfo.Client = okq.NewWithOpts(okq.Opts{
		RedisPool:     p,
		NotifyTimeout: g.OkqInfo.Timeout,
	})
}

func (g *GenAPI) contextHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		ctx := requestCtx(r)
		if len(ContextKV(ctx)) == 0 {
			ctx = ContextMergeKV(ctx, rpcutil.RequestKV(r))
		}

		// TODO I'll be posting a question in the google group about what
		// exactly we're supposed to be doing here. It's currently very unclear
		//cn, ok := w.(http.CloseNotifier)
		//if !ok {
		//	h.ServeHTTP(w, r)
		//	return
		//}
		//closeCh := make(chan struct{})
		//reqCloseCh := cn.CloseNotify()
		//ctx, cancelFn := context.WithCancel(ctx)
		//go func() {
		//	<-closeCh
		//	<-reqCloseCh
		//	cancelFn()
		//}()

		g.ctxsL.Lock()
		g.ctxs[r] = ctx
		g.ctxsL.Unlock()

		h.ServeHTTP(w, r)
		//close(closeCh)

		g.ctxsL.Lock()
		delete(g.ctxs, r)
		g.ctxsL.Unlock()
	})
}

// RequestContext returns a context for the given request. The context will be
// cancelled if the request is closed, and may possibly have a deadline on it as
// well
func (g *GenAPI) RequestContext(r *http.Request) context.Context {
	g.ctxsL.RLock()
	defer g.ctxsL.RUnlock()

	ctx := g.ctxs[r]
	if ctx == nil {
		ctx = context.Background()
	}
	return ctx
}

// ReloadListeners reloads the listener configurations of all existing
// listeners. This doesn't actually close the listen sockets, just hot reloads
// the configuration. Goes through each listener sequentially and returns the
// first error it encounters.
func (g *GenAPI) ReloadListeners() error {
	for _, lr := range g.listeners {
		if err := lr.Reload(); err != nil {
			return err
		}
	}
	return nil
}

// Call makes an rpc call, presumably to another genapi server but really it
// only has to be a JSONRPC2 server. If it is another genapi server, however,
// the given context will be propagated to it, as well as being used here as a
// timeout if deadline is set on it. See rpcutil for more on how the rest of the
// arguments work.
//
// Note that host can be a hostname, and address (host:port), or a url
// (http[s]://host[:port])
func (g *GenAPI) Call(ctx context.Context, res interface{}, host, method string, args interface{}) error {
	host = g.SRVClient.MaybeSRVURL(host)

	r, err := http.NewRequest("POST", host, nil)
	if err != nil {
		return err
	}
	ContextApply(r, ctx)

	opts := rpcutil.JSONRPC2Opts{
		BaseRequest: r,
		Context:     ctx,
	}

	return rpcutil.JSONRPC2CallOpts(opts, host, res, method, args)
}

func (g *GenAPI) remoteAPIAddr(remoteAPI string) string {
	addr, _ := g.ParamStr("--" + remoteAPI + "-addr")
	if addr == "" {
		llog.Fatal("no address defined", llog.KV{"api": remoteAPI})
	}
	return addr
}

// Caller provides a way of calling RPC methods against a pre-defined remote
// endpoint. The Call method is essentially the same as GenAPI's Call method,
// but doesn't take in a host parameter
type Caller interface {
	Call(ctx context.Context, res interface{}, method string, args interface{}) error
}

type caller struct {
	g    *GenAPI
	addr string
}

func (c caller) Call(ctx context.Context, res interface{}, method string, args interface{}) error {
	return c.g.Call(ctx, res, c.addr, method, args)
}

// RemoteAPIAddr returns an address to use for the given remoteAPI (which must
// be defined in RemoteAPIs). The address will have had SRV called on it
// already. A Fatal will be thrown if no address has been provided for the
// remote API
func (g *GenAPI) RemoteAPIAddr(remoteAPI string) string {
	return g.SRVClient.MaybeSRV(g.remoteAPIAddr(remoteAPI))
}

// RemoteAPICaller takes in the name of a remote API instance defined in the
// RemoteAPIs field, and returns a function which can be used to make RPC calls
// against it. The arguments to the returned function are essentially the same
// as those to the Call method, sans the host argument. A Fatal will be thrown
// if no address has been provided for the remote API
func (g *GenAPI) RemoteAPICaller(remoteAPI string) Caller {
	addr := g.remoteAPIAddr(remoteAPI)
	return caller{g, addr}
}

// CallerStub provides a convenient way to make stubbed endpoints for testing
type CallerStub func(method string, args interface{}) (interface{}, error)

// Call implements the Call method for the Caller interface. It passed method
// and args to the underlying CallerStub function. The returned interface from
// that function is assigned to res (if the underlying types for them are
// compatible). The passed in context is ignored.
func (cs CallerStub) Call(_ context.Context, res interface{}, method string, args interface{}) error {
	csres, err := cs(method, args)
	if err != nil {
		return err
	}

	if res == nil {
		return nil
	}

	vres := reflect.ValueOf(res).Elem()
	vres.Set(reflect.ValueOf(csres))
	return nil
}
*/
