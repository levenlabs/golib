package genapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/levenlabs/go-llog"
	"github.com/levenlabs/go-srvclient"
	"github.com/levenlabs/lrpc/lrpchttp/json2"
	"github.com/mediocregopher/lever"
	"golang.org/x/net/context"
	"golang.org/x/net/context/ctxhttp"
)

// Remoter provides functionality for interacting with other remote interfaces.
// Primarily it handles making SRV calls to resolve addresses.
type Remoter struct {
	remotes map[string]string // maps remote name to its base address (pre-srv)
	srv     *srvclient.SRVClient
}

// NewRemoter returns an initialized Remoter. Add should be called immediately
// after this to make the Remoter actually aware of other remotes
func NewRemoter() Remoter {
	// TODO datacenter stuff... somehow
	srv := &srvclient.SRVClient{}
	srv.EnableCacheLast()
	return Remoter{
		remotes: map[string]string{},
		srv:     srv,
	}
}

// Add makes the Remoter able to resolve a new remote interface. The remote is
// identified by name, and has a default address of the one given (can be empty
// string). This default address will be the one which has SRV called on it,
// unless overwritten by WithParams.
func (r Remoter) Add(name, def string) {
	r.remotes[name] = def
}

// Params returns the param definitions for all the remote interfaces which have
// been Add'd thusfar
func (r Remoter) Params() []lever.Param {
	var pp []lever.Param
	for name, def := range r.remotes {
		pp = append(pp, lever.Param{
			Name:        "--" + name,
			Description: "Address of " + name + ", which will be DNS SRV resolved if neededn",
			Default:     def,
		})
	}
	return pp
}

// WithParams populates its remotes with new addresses to resolve, if any were
// given in the lever.
func (r Remoter) WithParams(l *lever.Lever) {
	for name, def := range r.remotes {
		if addr, _ := l.ParamStr("--" + name); addr != "" {
			r.remotes[name] = addr
		} else if def == "" {
			llog.Fatal("remote address not set", llog.KV{"name": "--" + name})
		}
	}
}

// Addr returns a host:port address for the remote Add'd with the given name
func (r Remoter) Addr(name string) string {
	return r.srv.MaybeSRV(r.remotes[name])
}

// URL returns a *url.URL with the given scheme and path, and a host for the
// given name returned from Addr
func (r Remoter) URL(scheme, name, path string) *url.URL {
	return &url.URL{
		Scheme: scheme,
		Host:   r.Addr(name),
		Path:   path,
	}
}

// Caller provides a way of calling RPC methods against another remote endpoint
type Caller interface {
	// Call does the actual work. It will method and args are sent to the remote
	// endpoint, and the result is unmarshalled into res (which should be a
	// pointer).
	Call(ctx context.Context, res interface{}, method string, args interface{}) error
}

type remoterCaller struct {
	name string
	r    Remoter
}

// TODO confirm that we want path to be jsonrpc2
// TODO confirm what we want to do about context
// TODO X-Forwarded-For

func (rc remoterCaller) Call(ctx context.Context, res interface{}, method string, args interface{}) error {
	u := rc.r.URL("http", rc.name, "/jsonrpc2").String()
	kv := llog.KV{"url": u, "method": method}

	jr, err := json2.NewRequest(method, args)
	if err != nil {
		return llog.ErrWithKV(err, kv)
	}

	body := new(bytes.Buffer)
	if err := json.NewEncoder(body).Encode(jr); err != nil {
		return llog.ErrWithKV(err, kv)
	}

	r, err := http.NewRequest("POST", u, body)
	if err != nil {
		return llog.ErrWithKV(err, kv)
	}
	r.Header.Set("Content-Type", "application/json")

	resp, err := ctxhttp.Do(ctx, http.DefaultClient, r)
	if err != nil {
		return llog.ErrWithKV(err, kv)
	}
	defer resp.Body.Close()

	var jresp json2.Response
	jresp.Result = res
	// setting error isn't strictly necessary, but we want to decode Data into a
	// KV
	jresp.Error = &json2.Error{
		Data: llog.KV{},
	}
	if err := json.NewDecoder(resp.Body).Decode(&jresp); err != nil {
		return llog.ErrWithKV(err, kv)
	} else if jresp.Error.Message != "" {
		// TODO this is weird, there's a KV in this theoretically, but i'm not
		// sure if it ever actually gets pulled out at any point
		return jresp.Error
	}

	return nil
}

// Caller returns a Caller instance which will perform calls specifically for
// the given remote interface
func (r Remoter) Caller(name string) Caller {
	return remoterCaller{name: name, r: r}
}
