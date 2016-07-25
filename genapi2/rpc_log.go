package genapi

import (
	"fmt"
	"time"

	"github.com/levenlabs/go-llog"
	"github.com/levenlabs/golib/errctx"
	"github.com/levenlabs/lrpc"
	"github.com/levenlabs/lrpc/lrpchttp/json2"
)

type logErrKey int

const (
	logErrCode logErrKey = iota
	logErrQuiet
)

func errWithCode(err error, code json2.ErrorCode) error {
	return errctx.Set(err, logErrCode, code)
}

func quietErr(err error) error {
	return errctx.Set(err, logErrQuiet, true)
}

// this handler assumes a json2 Codec is being used
type logHandler struct {
	lrpc.Handler

	// if true internal server errors will be returned as generic rpc error
	hideServerErrors bool

	excludeMethodLog map[string]bool
}

func (lh logHandler) infoLog(method string) bool {
	if lh.excludeMethodLog == nil {
		return true
	}
	return !lh.excludeMethodLog[method]
}

func (lh logHandler) ServeRPC(c lrpc.Call) interface{} {
	ctx := c.GetContext()
	kv := ContextKV(ctx)

	var method string
	if r := json2.ContextRequest(ctx); r != nil {
		method = r.Method
		kv["method"] = r.Method
		if llog.GetLevel() == llog.DebugLevel {
			kv["args"] = string(*r.Params)
		}
		if lh.infoLog(method) {
			llog.Info("incoming rpc request", kv)
		}
	}

	start := time.Now()
	ret := lh.Handler.ServeRPC(c)
	kv["took"] = time.Since(start)

	err, ok := ret.(error)
	if !ok && lh.infoLog(method) {
		llog.Info("request response", kv)
		return ret
	}

	ekv := llog.ErrKV(err)
	kv = llog.Merge(kv, ekv)

	jsonErr := func() *json2.Error {
		if jerr, ok := err.(*json2.Error); ok {
			return jerr
		}
		jerr := &json2.Error{
			Code:    json2.ErrServer,
			Message: err.Error(),
			Data:    ekv,
		}
		if codei := errctx.Get(err, logErrCode); codei != nil {
			jerr.Code = codei.(json2.ErrorCode)
		}
		return jerr
	}()

	// Just so it's clear in the log, we modify the messsage a bit specifically
	// for server errors, also add code to kv
	if jsonErr.Code == json2.ErrServer {
		jsonErr.Message = fmt.Sprintf("internal server error: %s", jsonErr.Message)
	}
	kv["code"] = jsonErr.Code

	var quietErr bool
	if qi := errctx.Get(err, logErrQuiet); qi != nil {
		quietErr = qi.(bool)
	}

	// The only predefined error that is considered a server error really is
	// ErrServer, all the others which are less than it are basically client
	// errors. So all within this range are considered internal server errors,
	// and need to be possibly hidden and definitely output as errors
	if jsonErr.Code < 0 && jsonErr.Code >= json2.ErrServer {
		if lh.hideServerErrors {
			jsonErr = &json2.Error{
				Code:    json2.ErrServer,
				Message: "internal server error",
			}
		}
		llog.Error("jsonrpc internal server error", kv)
	} else if !quietErr {
		llog.Warn("jsonrpc client error", kv)
	}

	return jsonErr
}
