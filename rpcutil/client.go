package rpcutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/gorilla/rpc/v2/json2"
)

// JSONRPC2Request returns an http.Request which can be used to make the given
// call against the given url. body must be a pointer type
func JSONRPC2Request(url, method string, body interface{}) (*http.Request, error) {
	b, err := json2.EncodeClientRequest(method, body)
	if err != nil {
		return nil, err
	}

	r, err := http.NewRequest("POST", url, bytes.NewBuffer(b))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	return r, nil
}

// JSONRPC2Call will perform a JSON RPC2 call with the given method and
// marshalled body against the given url, and unmarshal the response into res.
// Any errors which occur will be returned. res and body must both be pointer
// types
func JSONRPC2Call(url string, res interface{}, method string, body interface{}) error {
	r, err := JSONRPC2Request(url, method, body)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(r)
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
	jbody := json.RawMessage(fmt.Sprintf(body, args...))
	return JSONRPC2Call(url, res, method, &jbody)
}

// JSONRPC2CallHandler is like JSONRPC2Call, except that the call will be made
// against the given http.Handler instead of an actual url
func JSONRPC2CallHandler(h http.Handler, res interface{}, method string, body interface{}) error {
	r, err := JSONRPC2Request("/", method, body)
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

// JSONRPC2RawCallHandler is like JSONRPC2RawCall, except that the call will be
// made against the given http.Handler instead of an actual url
func JSONRPC2RawCallHandler(h http.Handler, res interface{}, method, body string, args ...interface{}) error {
	jbody := json.RawMessage(fmt.Sprintf(body, args...))
	return JSONRPC2CallHandler(h, res, method, &jbody)
}
