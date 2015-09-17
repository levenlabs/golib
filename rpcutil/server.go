package rpcutil

import (
	"net/http"

	"github.com/gorilla/rpc/v2"
)

// JSONRPC2Handler returns an http.Handler which will handle JSON RPC2 calls
// directed at the given services. A service is a type that has a set of RPC
// methods on it which will be discovered and used by the rpc package.
func JSONRPC2Handler(codec rpc.Codec, service ...interface{}) http.Handler {
	s := rpc.NewServer()
	s.RegisterCodec(codec, "application/json")
	for i := range service {
		s.RegisterService(service[i], "")
	}
	return s
}
