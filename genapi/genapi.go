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
	"bytes"
	"crypto/sha1"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/context"

	"github.com/gorilla/rpc/v2"
	"github.com/levenlabs/gatewayrpc"
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

// Build variables which can be set during the go build command. E.g.:
// -ldflags "-X 'github.com/levenlabs/golib/genapi.BuildCommit commitHash'"
// These fields will be used to construct the string printed out when the
// --version flag is used.
var (
	BuildCommit    string
	BuildDate      string
	BuildGoVersion string
)

// Version compiles the build strings into a string which will be printed out
// when --version is used in a GenAPI instance, but is exposed so it may be used
// other places too.
func Version() string {
	orStr := func(s, alt string) string {
		if s == "" {
			return alt
		}
		return s
	}
	b := new(bytes.Buffer)
	fmt.Fprintf(b, "BuildCommit: %s\n", orStr(BuildCommit, "<unset>"))
	fmt.Fprintf(b, "BuildDate: %s\n", orStr(BuildDate, "<unset>"))
	fmt.Fprintf(b, "BuildGoVersion: %s\n", orStr(BuildGoVersion, "<unset>"))
	return b.String()
}

// MongoInfo contains information needed by the api to interact with a mongo
// backend, and also houses the connection to that backend (which can be
// interacted with through its methods)
type MongoInfo struct {
	// If you want to make mongo optional, set this and if --mongo-addr isn't
	// sent, then WithDB, WithColl will call fn with nil and SessionHelper's
	// session will be nil.
	Optional bool

	// The name of the mongo database this app should use. In TestMode this will
	// always be overwritten to "test_<DBName>"
	DBName string

	session *mgo.Session
}

// InitFunc is just a helper for a function that accepts a GenAPI pointer
type InitFunc func(*GenAPI)

// WithDB is similar to mgoutil.SessionHelper's WithDB, see those docs for more
// details
func (m *MongoInfo) WithDB(fn func(*mgo.Database)) {
	if m.session == nil {
		fn(nil)
		return
	}
	mgoutil.SessionHelper{
		Session: m.session,
		DB:      m.DBName,
	}.WithDB(fn)
}

// WithColl is similar to mgoutil.SessionHelper's WithColl, see those docs for
// more details
func (m *MongoInfo) WithColl(collName string, fn func(*mgo.Collection)) {
	if m.session == nil {
		fn(nil)
		return
	}
	mgoutil.SessionHelper{
		Session: m.session,
		DB:      m.DBName,
		Coll:    collName,
	}.WithColl(fn)
}

// CollSH returns an mgoutil.SessionHelper for a collection of the given name
// The SessionHelper's Session might be nil if you made mongo Optional.
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
	// If you want to make redis optional, set this and if --redis-addr isn't
	// sent, Cmder will be nil.
	Optional bool

	// Populated by the api once a connection to redis is made, and can be used
	// as such. Do not set manually.
	util.Cmder
}

// OkqInfo is used to tell the api to interact with a set of okq instances.
type OkqInfo struct {
	// If you want to make okq optional, set this and if --okq-addr isn't sent,
	// Client will return nil
	Optional bool

	// Read/Write timeout for redis connection and the NotifyTimeout for Client.
	// Defaults to 30 seconds
	// Do not change after initializing GenAPI
	Timeout time.Duration

	*okq.Client
}

// TLSInfo is used to tell the api to use TLS (e.g. https/ssl) when listening
// for incoming requests
type TLSInfo struct {
	// If set to true then the config options for passing in cert files on the
	// command-line will not be used, and instead the Certs field will be
	// expected to be filled in manually during the Init function
	FillCertsManually bool

	// One or more certificates to use for TLS. Will be filled automatically if
	// FillCertsManually is false
	Certs []tls.Certificate

	// SessionTicketKey is used by TLS servers to provide session
	// resumption. If multiple servers are terminating connections for the same
	// host they should all have the same SessionTicketKey. If the
	// SessionTicketKey leaks, previously recorded and future TLS connections
	// using that key are compromised.
	SessionTicketKey [32]byte
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

	// The set of rpc service structs which this API will host. Must have at
	// least one service in APIMode
	Services []interface{}

	// Like Services, but these will not be registered with the underlying
	// gateway library, and therefore will not show up in calls to
	// "RPC.GetMethods"
	HiddenServices []interface{}

	// The mux which the rpc services will be added to. If not set a new one
	// will be created and used. This can be used to provide extra functionality
	// in conjunction with the RPC server, or completely in place of it.
	//
	// It is important that RPCEndpoint does *not* have a handler set in this
	// mux, as GenAPI will be setting it itself.
	Mux *http.ServeMux

	// The http endpoint that the RPC handler for Services and HiddenServices
	// will be attached to. Defaults to "/". If you set this to "_", no rpc
	// listener will be set up and its up to you to add the handler from RPC()
	// to the mux for whatever path you need.
	RPCEndpoint string

	// Additional lever.Param structs which can be included in the lever parsing
	LeverParams []lever.Param

	// If mongo is intended to be used as a backend, this should be filled in
	*MongoInfo

	// If redis is intended to be used, this should be filled in.
	*RedisInfo

	// If okq is intended to be used, this should be filled in.
	*OkqInfo

	// If TLS is intended to be used, this should be filled in. The Certs field
	// of TLSInfo may be filled in during the Init function for convenience, but
	// the struct itself must be initialized before any of the Mode methods are
	// called
	*TLSInfo

	// A function to run just after initializing connections to backing
	// database. Meant for performing any initialization needed by the app.
	// This is called before any AppendInit functions
	Init InitFunc

	inits []InitFunc

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

	// Optional set of Healthers which should be checked during a /health-check.
	// These will be checked sequentially, and if any return an error that will
	// be logged and the health check will return false. The key in the map is a
	// name for the Healther which can be logged
	Healthers map[string]Healther

	// SRVClient which will be used by GenAPI when resolving requests, and which
	// can also be used by other processes as well. This should only be modified
	// during the init function
	srvclient.SRVClient

	ctxs  map[*http.Request]context.Context
	ctxsL sync.RWMutex

	// Mutex for accessing Healthers
	healthersL sync.Mutex

	// Signal channel. Included for testing purposes only
	sigCh chan os.Signal

	// set of active listeners for this genapi (APIMode only)
	listeners []*listenerReloader

	// the active httpWaiter for the instance
	hw *httpWaiter

	countCh chan bool

	httpClient *http.Client

	// generated during init based on --private-cidrs
	privateCIDRs cidrSet
}

type cidrSet []*net.IPNet

// returns nil if the cidrSet contains the address, or an error if it doesn't or
// the address is otherwise invalid.
func (c cidrSet) hasAddr(addr string) error {
	ipStr, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid addr %q: %s", addr, err)
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return fmt.Errorf("invalid ip %q", ipStr)
	}

	for _, cidr := range c {
		if cidr.Contains(ip) {
			return nil
		}
	}
	return fmt.Errorf("invalid ip %q", ipStr)
}

// The different possible Mode values for GenAPI
const (
	APIMode  = "api"
	TestMode = "test"
	CLIMode  = "cli"
)

// HTTPDefaultClient returns a *http.Client with sane defaults that can be
// overridden on a case-by-case basis if you can't use http.DefaultClient
func HTTPDefaultClient() *http.Client {
	// based on defaults in 1.7
	return &http.Client{Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConnsPerHost: 100,
		// even though this is high, we should keep some number that isn't
		// infinity (default)
		MaxIdleConns:          1000,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}}
}

// APIMode puts the GenAPI into APIMode, wherein it listens for any incoming rpc
// requests and tries to serve them against one of its Services. This method
// will block indefinitely
func (g *GenAPI) APIMode() {
	g.Mode = APIMode
	g.sigCh = make(chan os.Signal, 1)
	g.init()
	g.RPCListen()

	// Once ListenAddr is populated with the final value we can call doSkyAPI
	skyapiStopCh := g.doSkyAPI()
	unhealthyTimeout, _ := g.ParamInt("--unhealthy-timeout")

	if g.InitDoneCh != nil {
		close(g.InitDoneCh)
	}

	// After this point everything is listening and we're just waiting for a
	// kill signal

	llog.Info("waiting for close signal")

	signal.Notify(g.sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-g.sigCh
	llog.Info("signal received, stopping")
	g.healthersL.Lock()
	g.Healthers = map[string]Healther{
		"sneezy": sneezy{},
	}
	g.healthersL.Unlock()
	if skyapiStopCh != nil {
		llog.Info("stopping skyapi connection")
		close(skyapiStopCh)
		// Wait a bit just in case something gets the skydns record before we
		// kill the skyapi connection, but the connection doesn't come in till
		// after hw.wait() runs
		time.Sleep(500 * time.Millisecond)
	}
	// Appear as unhealthy for a while before hw.wait() runs
	time.Sleep(time.Duration(unhealthyTimeout) * time.Millisecond)
	g.hw.wait() // hw is populated in RPCListen
	time.Sleep(50 * time.Millisecond)

	if g.DoneCh != nil {
		close(g.DoneCh)
	}

}

func (g *GenAPI) privateOnlyHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := g.privateCIDRs.hasAddr(r.RemoteAddr); err != nil {
			http.Error(w, "", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func (g *GenAPI) countHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.countCh <- true
		h.ServeHTTP(w, r)
	})
}

func (g *GenAPI) hostnameHandler(h http.Handler) http.Handler {
	hostname, _ := g.ParamStr("--hostname")
	if hostname == "" {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Hostname", hostname)
		h.ServeHTTP(w, r)
	})
}

// RPCListen sets up listeners for the GenAPI listen and starts them up. This
// may only be called after TestMode or CLIMode has been called, it is
// automatically done for APIMode.
func (g *GenAPI) RPCListen() {
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

	if g.RPCEndpoint != "_" {
		g.Mux.Handle(g.RPCEndpoint, g.privateOnlyHandler(g.RPC()))
	}
	// The net/http/pprof package expects to be under /debug/pprof/, which is
	// why we don't strip the prefix here
	g.Mux.Handle("/debug/pprof/", g.privateOnlyHandler(g.pprofHandler()))
	g.Mux.Handle("/debug/loglevel", g.privateOnlyHandler(logLevelHandler))
	g.Mux.Handle("/health-check", g.healthCheck())

	g.hw = &httpWaiter{
		ch: make(chan struct{}, 1),
	}

	var h http.Handler
	h = g.Mux
	h = g.countHandler(h)
	h = g.hostnameHandler(h)
	h = g.contextHandler(h)
	h = g.hw.handler(h)

	addrs, _ := g.Lever.ParamStrs("--listen-addr")
	for _, addr := range addrs {
		// empty addr might get passed in to disable --listen-addr
		if addr == "" {
			continue
		}
		g.listeners = append(g.listeners, g.serve(h, addr, false))
	}

	if g.TLSInfo != nil {
		addrs, _ := g.Lever.ParamStrs("--tls-listen-addr")
		for _, addr := range addrs {
			if addr == "" {
				continue
			}
			g.listeners = append(g.listeners, g.serve(h, addr, true))
		}
	}
}

// AddHealther adds a healther to Healthers under the specified key
func (g *GenAPI) AddHealther(key string, healther Healther) {
	g.healthersL.Lock()
	defer g.healthersL.Unlock()
	g.Healthers[key] = healther
}

// This starts a go-routine which will do the actual serving of the handler
func (g *GenAPI) serve(h http.Handler, addr string, doTLS bool) *listenerReloader {
	kv := llog.KV{"addr": addr, "tls": doTLS}
	llog.Info("creating listen socket", kv)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		llog.Fatal("failed creating listen socket", kv.Set("err", err))
	}
	actualAddr := ln.Addr().String()
	kv["addr"] = actualAddr

	// If this is the first address specified set ListenAddr to that, so it will
	// be advertised with skyapi
	if g.ListenAddr == "" {
		g.ListenAddr = actualAddr
	}

	var tc *tls.Config
	if doTLS {
		// when srv.Serve is called, it checks to see if NextProtos contains
		// "h2" and if so, it sets up *tls.Config to be http2-ready
		tc = &tls.Config{
			NextProtos:       []string{"h2", "http/1.1"},
			SessionTicketKey: g.TLSInfo.SessionTicketKey,
		}
	}

	netln := net.Listener(tcpKeepAliveListener{ln.(*net.TCPListener)})
	lr, err := newListenerReloader(netln, g.listenerMaker(tc))
	if err != nil {
		llog.Fatal("failed to create listener", kv.Set("err", err))
	}

	go func() {
		llog.Info("starting rpc listening", kv)
		srv := &http.Server{
			Handler:   h,
			TLSConfig: tc,
		}
		srv.Serve(lr)
	}()

	return lr
}

func (g *GenAPI) listenerMaker(tc *tls.Config) func(net.Listener) (net.Listener, error) {
	return func(l net.Listener) (net.Listener, error) {
		l = newProxyListener(l, g.privateCIDRs)
		if tc != nil {
			// BuildNameToCertificate creates the map and THEN loops over
			// and adds to the map, this creates a race condition, so we use
			// a copy and add it back to the pointer after
			tcc := &tls.Config{
				Certificates: g.TLSInfo.Certs,
			}
			tcc.BuildNameToCertificate()
			tc.Certificates = tcc.Certificates
			tc.NameToCertificate = tcc.NameToCertificate
			// use the same value for tc across listeners so we can keep session
			// tickets ther same
			l = tls.NewListener(l, tc)
		}
		return l, nil
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

// RPC returns an http.Handler which will handle the RPC calls made against it
// for the GenAPI's Services
func (g *GenAPI) RPC() http.Handler {
	// TODO make gatewayrpc.Server have an option not to do its logging
	// per-request, so we can do it in here with the proper KVs from the context
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

func (g *GenAPI) pprofHandler() http.Handler {
	h := http.NewServeMux()
	h.Handle("/debug/pprof/", http.HandlerFunc(pprof.Index))

	// Even though Index handles this, this particular one won't work without
	// setting the BlockProfileRate temporarily.
	h.HandleFunc("/debug/pprof/block", func(w http.ResponseWriter, r *http.Request) {
		runtime.SetBlockProfileRate(1)
		time.Sleep(5 * time.Second)
		pprof.Index(w, r)
		runtime.SetBlockProfileRate(0)
	})

	h.Handle("/debug/pprof/cmdline", http.HandlerFunc(pprof.Cmdline))
	h.Handle("/debug/pprof/profile", http.HandlerFunc(pprof.Profile))
	h.Handle("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol))
	h.Handle("/debug/pprof/trace", http.HandlerFunc(pprof.Trace))
	return h
}

// GET /loglevel?level=<newlevel>
var logLevelHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if l := r.FormValue("level"); l != "" {
		if err := llog.SetLevelFromString(l); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		fmt.Fprintf(w, "New log level is %s\n", llog.GetLevel())
		return
	}

	fmt.Fprintf(w, "Current log level is %s\n", llog.GetLevel())
})

func (g *GenAPI) init() {
	g.ctxs = map[*http.Request]context.Context{}
	rpcutil.InstallCustomValidators()
	g.SRVClient.EnableCacheLast()
	g.doLever()

	g.SRVClient.Preprocess = g.srvClientPreprocess
	g.httpClient = HTTPDefaultClient()

	// generate privateCIDRS
	{
		privateCIDRsStr, _ := g.ParamStr("--private-cidrs")
		privateCIDRs := strings.Split(privateCIDRsStr, ",")
		for _, cidr := range privateCIDRs {
			if cidr == "" {
				continue
			}
			_, parsed, err := net.ParseCIDR(cidr)
			if err != nil {
				llog.Fatal("could not parse private cidr", llog.KV{"cidr": cidr}, llog.ErrKV(err))
			}
			g.privateCIDRs = append(g.privateCIDRs, parsed)
		}
	}

	if g.RPCEndpoint == "" {
		g.RPCEndpoint = "/"
	}

	if g.Mux == nil {
		g.Mux = http.NewServeMux()
	}

	if g.Lever.ParamFlag("--version") {
		fmt.Print(Version())
		os.Stdout.Sync()
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
	if g.TLSInfo != nil {
		key, _ := g.ParamStr("--tls-session-key")
		if key != "" {
			kb := []byte(key)
			if len(kb) != 32 {
				llog.Fatal("--tls-session-key must be 32 bytes", llog.KV{"len": len(kb)})
			}
			copy(g.TLSInfo.SessionTicketKey[:], kb)
		}
	}

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

	g.Lever.Add(lever.Param{
		Name:        "--hostname",
		Description: "What hostanme the service is running on",
		Default:     os.Getenv("HOSTNAME"),
	})

	// The listen-addr parameters can be used outside of APIMode through the
	// RPCListener method
	g.Lever.Add(lever.Param{
		Name:         "--listen-addr",
		Description:  "[address]:port to listen for requests on. If port is zero a port will be chosen randomly",
		DefaultMulti: []string{":0"},
	})
	g.Lever.Add(lever.Param{
		Name:        "--private-cidrs",
		Description: "Comma separated list of cidrs which are considered to be our private network",
		Default:     "127.0.0.1/32,::1/128,10.0.0.0/8",
	})
	if g.TLSInfo != nil {
		g.Lever.Add(lever.Param{
			Name:         "--tls-listen-addr",
			Description:  "[address]:port to listen for https requests on. If port is zero a port will be chosen randomly",
			DefaultMulti: []string{},
		})
		g.Lever.Add(lever.Param{
			Name:        "--tls-session-key",
			Description: "If multiple servers are terminating connections for the same host they should all have the same key",
			Default:     "",
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
			Name:        "--unhealthy-timeout",
			Description: "Number of milliseconds to appear unhealthy after a stop signal is received",
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

	g.Lever.Add(lever.Param{
		Name:        "--version",
		Aliases:     []string{"-V"},
		Description: "Print out version information for this binary",
		Flag:        true,
	})

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
				llog.Warn("skyapi error", kv.Set("err", err))
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

// CallErr is an implementation of error which is returned from the Call method,
// and subsequently by the Call methods on Callers returned by RemoteAPICaller
// and NewCaller. If used, CallErr will not be a pointer
type CallErr struct {
	URL    string
	Method string
	Err    error
}

func (c CallErr) Error() string {
	return fmt.Sprintf("calling %q on %q: %s", c.Method, c.URL, c.Err)
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
		return CallErr{URL: host, Method: method, Err: err}
	}
	ContextApply(r, ctx)

	opts := rpcutil.JSONRPC2Opts{
		BaseRequest: r,
		Context:     ctx,
		Client:      g.httpClient,
	}

	if err := rpcutil.JSONRPC2CallOpts(opts, host, res, method, args); err != nil {
		return CallErr{URL: host, Method: method, Err: err}
	}
	return nil
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

// NewCaller returns an instance of a Caller which will make RPC requests
// against the given address, after doing a SRV request on it before each
// request
func (g *GenAPI) NewCaller(addr string) Caller {
	return caller{g, addr}
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

type retryCaller struct {
	Caller
	attempts int
}

// RetryCaller returns a Caller which wraps the given one, passing all Calls
// back to it. If any return any errors they will be retried the given number of
// times until one doesn't return an error. The most recent error is returned if
// all attempts fail.
func RetryCaller(c Caller, attempts int) Caller {
	return retryCaller{c, attempts}
}

func (rc retryCaller) Call(ctx context.Context, res interface{}, method string, args interface{}) error {
	var err error
	for i := 0; i < rc.attempts; i++ {
		if err = rc.Caller.Call(ctx, res, method, args); err == nil {
			return nil
		}
	}
	return err
}
