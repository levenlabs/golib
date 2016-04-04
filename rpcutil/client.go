package rpcutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"

	"golang.org/x/net/context"
	"golang.org/x/net/context/ctxhttp"

	"github.com/gorilla/rpc/v2/json2"
)

// JSONRPC2Opts can be passed into any of the JSONRPC2*Opts calls to later their
// behavior in some way. All fields are optional, see each one for a description
// of what it does.
type JSONRPC2Opts struct {

	// If set this will be used as the request which is sent to the JSONRPC2
	// server. It will be used as is, except:
	//
	// * Method will be set to POST
	// * URL will be set to whatever is passed in
	// * Body will be set to the proper request body
	// * Content-Type header will be set to application/json
	//
	// This is useful if there are other headers or setting you'd like to have
	// on your request going out, such as X-Forwarded-For
	BaseRequest *http.Request

	// If set, this will be used as the context for http request being made.
	// The call will respect the cancellation of the context
	Context context.Context
}

// JSONPRC2RequestOpts is like JSONRPC2Request but it takes in a JSONRPC2Opts
// struct to modify behavior
func JSONRPC2RequestOpts(opts JSONRPC2Opts, urlStr, method string, body interface{}) (*http.Request, error) {
	b, err := json2.EncodeClientRequest(method, body)
	if err != nil {
		return nil, err
	}

	var r *http.Request
	if opts.BaseRequest != nil {
		u, err := url.Parse(urlStr)
		if err != nil {
			return nil, err
		}
		r = opts.BaseRequest
		r.Method = "POST"
		r.URL = u
		r.Body = ioutil.NopCloser(bytes.NewBuffer(b))
	} else {
		r, err = http.NewRequest("POST", urlStr, bytes.NewBuffer(b))
		if err != nil {
			return nil, err
		}
	}

	r.Header.Set("Content-Type", "application/json")
	return r, nil
}

// JSONRPC2Request returns an http.Request which can be used to make the given
// call against the given url. body must be a pointer type
func JSONRPC2Request(url, method string, body interface{}) (*http.Request, error) {
	return JSONRPC2RequestOpts(JSONRPC2Opts{}, url, method, body)
}

// JSONRPC2CallOpts is like JSONRPC2Call but it takes in a JSONRPC2Opts struct
// to modify behavior
func JSONRPC2CallOpts(opts JSONRPC2Opts, url string, res interface{}, method string, body interface{}) error {
	r, err := JSONRPC2RequestOpts(opts, url, method, body)
	if err != nil {
		return err
	}

	if opts.Context == nil {
		opts.Context = context.Background()
	}

	resp, err := ctxhttp.Do(opts.Context, http.DefaultClient, r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	err = json2.DecodeClientResponse(resp.Body, &res)
	if err != nil {
		return err
	}
	return nil
}

// JSONRPC2Call will perform a JSON RPC2 call with the given method and
// marshalled body against the given url, and unmarshal the response into res.
// Any errors which occur will be returned. res and body must both be pointer
// types
func JSONRPC2Call(url string, res interface{}, method string, body interface{}) error {
	return JSONRPC2CallOpts(JSONRPC2Opts{}, url, res, method, body)
}

// JSONRPC2RawCallOpts is like JSONRPC2RawCall but it takes in a JSONRPC2Opts
// struct to modify behavior
func JSONRPC2RawCallOpts(
	opts JSONRPC2Opts, url string, res interface{}, method, body string,
	args ...interface{},
) error {
	jbody := json.RawMessage(fmt.Sprintf(body, args...))
	return JSONRPC2CallOpts(opts, url, res, method, &jbody)
}

// JSONRPC2RawCall will perform a JSON RPC2 call with the given method,
// and a body formed using fmt.Sprintf(body, args...), against the given
// http.Handler. The result will be unmarshalled into res, which should be a
// pointer type. Any errors at any point in the process will be returned
//
//	res := struct{
//		Foo int `json:"foo"`
//	}{}
//	err := rpcutil.JSONRPC2RawCall(
//		"http://localhost/",
//		&res,
//		"Foo.Bar",
//		`{
//			"Arg1":"%s",
//			"Arg2":"%d",
//		}`,
//		"some value for Arg1",
//		5, // value for Arg2
//	)
//
func JSONRPC2RawCall(url string, res interface{}, method, body string, args ...interface{}) error {
	return JSONRPC2RawCallOpts(JSONRPC2Opts{}, url, res, method, body, args...)
}

// JSONRPC2CallHandlerOpts is like JSONRPC2CallHandler but it takes in a
// JSONRPC2Opts struct to modify behavior
func JSONRPC2CallHandlerOpts(
	opts JSONRPC2Opts, h http.Handler, res interface{}, method string, body interface{},
) error {
	r, err := JSONRPC2RequestOpts(opts, "/", method, body)
	if err != nil {
		return err
	}

	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, r)

	err = json2.DecodeClientResponse(resp.Body, &res)
	if err != nil {
		return err
	}
	return nil
}

// JSONRPC2CallHandler is like JSONRPC2Call, except that the call will be made
// against the given http.Handler instead of an actual url
func JSONRPC2CallHandler(h http.Handler, res interface{}, method string, body interface{}) error {
	return JSONRPC2CallHandlerOpts(JSONRPC2Opts{}, h, res, method, body)
}

// JSONRPC2RawCallHandlerOpts is like JSONRPC2RawCallHandler but it takes in a
// JSONRPC2Opts struct to modify behavior
func JSONRPC2RawCallHandlerOpts(
	opts JSONRPC2Opts, h http.Handler, res interface{}, method, body string,
	args ...interface{},
) error {
	jbody := json.RawMessage(fmt.Sprintf(body, args...))
	return JSONRPC2CallHandlerOpts(opts, h, res, method, &jbody)
}

// JSONRPC2RawCallHandler is like JSONRPC2RawCall, except that the call will be
// made against the given http.Handler instead of an actual url
func JSONRPC2RawCallHandler(h http.Handler, res interface{}, method, body string, args ...interface{}) error {
	return JSONRPC2RawCallHandlerOpts(JSONRPC2Opts{}, h, res, method, body, args...)
}
