package genapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"

	"github.com/levenlabs/go-llog"
	"github.com/levenlabs/lrpc/lrpchttp/json2"
	"golang.org/x/net/context"
	"golang.org/x/net/context/ctxhttp"
)

// RPCCaller is an interface which makes RPC calls against a single logical
// endpoint
type RPCCaller interface {
	// Call does the actual work. It will method and args are sent to the remote
	// endpoint, and the result is unmarshalled into res (which should be a
	// pointer).
	Call(ctx context.Context, res interface{}, method string, args interface{}) error
}

// RPCCallerStub provides a convenient way to make stubbed endpoints for testing
type RPCCallerStub func(method string, args interface{}) (interface{}, error)

// Call implements the Call method for the Caller interface. It passed method
// and args to the underlying RPCCallerStub function. The returned interface
// from that function is assigned to res (if the underlying types for them are
// compatible). The passed in context is ignored.
func (rcs RPCCallerStub) Call(_ context.Context, res interface{}, method string, args interface{}) error {
	csres, err := rcs(method, args)
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

// LLRPCCaller is Leven Lab's implementation of RPCCaller that it uses in
// conjunction with its RPC services
type LLRPCCaller struct {
	Remote
}

// Call implements the RPCCaller interface
// TODO confirm what we want to do about context
func (ll LLRPCCaller) Call(ctx context.Context, res interface{}, method string, args interface{}) error {
	addr, err := ll.Remote.Addr()
	if err != nil {
		return err
	}

	// TODO confirm that we want path to be jsonrpc2
	u := &url.URL{Schem: "http", Host: addr, Path: "/jsonrpc2"}
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
	// TODO X-Forwarded-For
	r.Header.Set("Content-Type", "application/json")

	// TODO we don't need to do this this way anymore
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
