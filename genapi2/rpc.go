package genapi

import "github.com/levenlabs/lrpc"
import "gopkg.in/validator.v2"

// DefaultAppliers is the set of functions which will be applied to all incoming
// rpc arguments by default, if none are otherwise set in RPCTpl
var DefaultAppliers = [](func(interface{}) error){
	validator.Validate,
}

// RPCTpl describes what additions will be made to a passed in lrpc.Handler when
// creating an rpc endpoint
//
// Each of the fields are Handlers which may be filled in, and which have
// various options which may be changed accordingly. If a Handler is left nil
// than it won't be used. The Handler field on each Handler should not be set.
type RPCTpl struct {
	// If true then server errors returned to the client will be replaced with a
	// generic message
	HideServeErrors bool

	// All method names in this slice will not be logged at all when they occur
	ExcludeMethodLog []string

	// Appliers is a list of functions which will be run on incoming arguments
	// after they have been unmarshalled. If nil, defaults to DefaultAppliers.
	Appliers []func(interface{}) error
}

// Handler takes in an underlying lrpc.Handler and wraps it according to the
// RPCTpl's fields, returning the wrapped handler.
func (rtpl RPCTpl) Handler(h lrpc.Handler) lrpc.Handler {
	if rtpl.Appliers == nil {
		rtpl.Appliers = DefaultAppliers
	}

	for _, a := range rtpl.Appliers {
		h = argsApplyHandler{Handler: h, apply: a}
	}

	exMethod := map[string]bool{}
	for _, m := range rtpl.ExcludeMethodLog {
		exMethod[m] = true
	}

	h = logHandler{
		Handler:          h,
		hideServerErrors: rtpl.HideServeErrors,
		excludeMethodLog: exMethod,
	}

	return h
}
