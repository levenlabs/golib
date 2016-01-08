// Package rpcutil provides various methods for working with gorilla's JSON RPC
// 2 interface (http://www.gorillatoolkit.org/pkg/rpc/v2/json2)
package rpcutil

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"

	"gopkg.in/validator.v2"

	"github.com/gorilla/rpc/v2"
	"github.com/gorilla/rpc/v2/json2"
	"github.com/levenlabs/go-llog"
)

// RequestKV returns a basic KV for passing into llog, filled with entries
// related to the passed in http.Request
func RequestKV(r *http.Request) llog.KV {
	// TODO maybe handle X-Forwarded-For? gotta talk to james
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return llog.KV{
		"ip": ip,
	}
}

// we don't ever really pass this into encoding/json, so having it implement
// json.Marshaler isn't really necessary, but it's helpful to think of it in
// this way
type jsonInliner struct {
	orig  interface{}
	extra map[string]interface{}
}

func (j jsonInliner) MarshalJSON() ([]byte, error) {
	bOrig, err := json.Marshal(j.orig)
	if err != nil {
		return nil, err
	}
	if len(bOrig) < 2 || bOrig[len(bOrig)-1] != '}' {
		return nil, errors.New("jsonInliner original value not an object")
	}
	if len(j.extra) == 0 {
		return bOrig, nil
	}

	bExtra, err := json.Marshal(j.extra)
	if err != nil {
		return nil, err
	}

	bOrig = bOrig[:len(bOrig)-1]
	bOrig = append(bOrig, ',')
	bOrig = append(bOrig, bExtra[1:]...)
	return bOrig, nil
}

// LLCodec wraps around gorilla's json2.Codec, adding logging to all requests
type LLCodec struct {
	c rpc.Codec

	// If true any errors which are not user caused (error code < 1) will not
	// actually be returned to the client, only a generic error message in their
	// place
	HideServerErrors bool

	// If true the gopkg.in/validator.v2 package will be used to automatically
	// validate inputs to calls
	ValidateInput bool

	// If set, once a non-error response is returned by an rpc endpoint this
	// will be called and the result (if non-nil) will be inlined with the
	// original response. The original response must encode to a json object for
	// this to work.
	//
	// For example, if the original response encodes to `{"success":true}`, and
	// ResponseInliner returns `{"currentTime":123456}`, the final response sent
	// to the client will be `{"success":true,"currentTime":123456}`
	ResponseInliner func(*http.Request) map[string]interface{}
}

// NewLLCodec returns an LLCodec, which is an implementation of rpc.Codec around
// json2.Codec. All public fields on LLCodec can be modified up intil passing
// this into rpc.RegisterCodec
func NewLLCodec() LLCodec {
	return LLCodec{c: json2.NewCodec()}
}

// NewRequest implements the NewRequest method for the rpc.Codec interface
func (c LLCodec) NewRequest(r *http.Request) rpc.CodecRequest {
	return llCodecRequest{
		c:            &c,
		CodecRequest: c.c.NewRequest(r),
		r:            r,
		kv:           RequestKV(r),
	}
}

type llCodecRequest struct {
	c *LLCodec
	rpc.CodecRequest
	r  *http.Request
	kv llog.KV
}

func (cr llCodecRequest) ReadRequest(args interface{}) error {
	// After calling the underlying ReadRequest the args will be filled in
	if err := cr.CodecRequest.ReadRequest(args); err != nil {
		// err will already be a json2.Error in this specific case, we don't
		// have to wrap it again
		return err
	}

	cr.kv["method"], _ = cr.CodecRequest.Method()
	var fn llog.LogFunc
	if llog.GetLevel() == llog.DebugLevel {
		cr.kv["args"] = fmt.Sprintf("%+v", args)
		fn = llog.Debug
	} else {
		fn = llog.Info
	}

	if cr.c.ValidateInput {
		if err := validator.Validate(args); err != nil {
			return &json2.Error{
				Code:    json2.E_BAD_PARAMS,
				Message: err.Error(),
			}
		}
	}

	fn("jsonrpc incoming request", cr.kv)

	return nil
}

func (cr llCodecRequest) maybeInlineExtra(r interface{}) (interface{}, error) {
	if cr.c.ResponseInliner == nil {
		return r, nil
	}
	extra := cr.c.ResponseInliner(cr.r)
	if extra == nil {
		return r, nil
	}

	j := jsonInliner{orig: r, extra: extra}
	b, err := j.MarshalJSON()
	if err != nil {
		return nil, err
	}
	jr := json.RawMessage(b)
	return &jr, nil
}

func (cr llCodecRequest) WriteResponse(w http.ResponseWriter, r interface{}) {
	if llog.GetLevel() == llog.DebugLevel {
		cr.kv["response"] = fmt.Sprintf("%+v", r)
		llog.Debug("jsonrpc responding", cr.kv)
	}

	newR, err := cr.maybeInlineExtra(r)
	if err != nil {
		cr.kv["err"] = err
		cr.kv["orig"], _ = json.Marshal(r)
		llog.Error("jsonrpc could not inline extra", cr.kv)
	} else {
		r = newR
	}

	cr.CodecRequest.WriteResponse(w, r)
}

func (cr llCodecRequest) WriteError(w http.ResponseWriter, status int, err error) {
	// status is ignored by gorilla

	cr.kv["err"] = err

	jsonErr, ok := err.(*json2.Error)
	if !ok {
		jsonErr = &json2.Error{
			Code:    json2.E_SERVER,
			Message: fmt.Sprintf("unexpected internal server error: %s", err),
		}
	}
	if kv, ok := jsonErr.Data.(llog.KV); ok {
		for k, v := range kv {
			cr.kv[k] = v
		}
	}

	// The only predefined error that is considered a server error really is
	// E_SERVER, all the others which are less than it are basically client
	// errors. So all within this range are considered internal server errors,
	// and need to be possibly hidden and definitely output as errors
	if jsonErr.Code < 0 && jsonErr.Code >= json2.E_SERVER {
		if cr.c.HideServerErrors {
			jsonErr = &json2.Error{
				Code:    json2.E_SERVER,
				Message: "internal server error",
			}
		}
		llog.Error("jsonrpc internal server error", cr.kv)
	} else {
		llog.Warn("jsonrpc client error", cr.kv)
	}

	cr.CodecRequest.WriteError(w, status, jsonErr)
}
