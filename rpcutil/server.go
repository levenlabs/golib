package rpcutil

import (
	"net/http"

	"github.com/gorilla/rpc/v2"
	"github.com/levenlabs/gatewayrpc"
)

// JSONRPC2Handler returns an http.Handler which will handle JSON RPC2 calls
// directed at the given services. A service is a type that has a set of RPC
// methods on it which will be discovered and used by the rpc package.
func JSONRPC2Handler(codec rpc.Codec, service ...interface{}) http.Handler {
	s := gatewayrpc.NewServer()
	s.RegisterCodec(codec, "application/json")
	for i := range service {
		s.RegisterService(service[i], "")
	}
	return s
}
