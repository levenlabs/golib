package genapi

import "github.com/levenlabs/lrpc"

type argsApplyHandler struct {
	lrpc.Handler
	apply func(args interface{}) error
}

func (aah argsApplyHandler) ServeRPC(c lrpc.Call) interface{} {
	return aah.Handler(argsApplyCall{
		Call: c,
		aah:  aah,
	})
}

type argsApplyCall struct {
	lrpc.Call
	aah argsApplyHandler
}

func (aac argsApplyCall) UnmarshalArgs(i interface{}) error {
	if err := aac.Call.UnmarshalArgs(i); err != nil {
		return err
	}

	aac.aah.apply(i)
}
