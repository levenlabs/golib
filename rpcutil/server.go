package rpcutil

import (
	"net/http"
	"reflect"

	"github.com/gorilla/rpc/v2"
	"github.com/levenlabs/go-llog"
)

// JSONRPC2Handler returns an http.Handler which will handle JSON RPC2 calls
// directed at the given services. A service is a type that has a set of RPC
// methods on it which will be discovered and used by the rpc package.
//
// This method is super deprecated and very dumb, don't use it
func JSONRPC2Handler(codec rpc.Codec, service ...interface{}) http.Handler {
	s := rpc.NewServer()
	s.RegisterCodec(codec, "application/json")
	for i := range service {
		if err := s.RegisterService(service[i], ""); err != nil {
			llog.Fatal("error registering service", llog.KV{
				"service": reflect.TypeOf(service[i]).String(),
				"err":     err,
			})
		}
	}
	return s
}
