package rpcutil

import (
	"net/http"

	"github.com/gorilla/rpc/v2"
)

// JSONRPC2Handler returns an http.Handler which will handle JSON RPC2 calls
// directed at the given services
func JSONRPC2Handler(codec rpc.Codec, service ...interface{}) http.Handler {
	s := rpc.NewServer()
	s.RegisterCodec(codec, "application/json")
	for i := range service {
		s.RegisterService(service[i], "")
	}
	return s
}
