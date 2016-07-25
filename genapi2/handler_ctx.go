package genapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/levenlabs/go-llog"
	"github.com/levenlabs/golib/rpcutil"
	"github.com/levenlabs/lrpc/lrpchttp"

	"golang.org/x/net/context"
)

const (
	requestCtxKVHeader = "X-LL-CTX-KV"
)

type ctxKey int

const ctxKVKey ctxKey = 0

func contextHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// TODO use the Request's context when go 1.7 is out
		ctx := context.TODO()

		if kv64 := r.Header.Get(requestCtxKVHeader); kv64 != "" {
			if kvStr, err := base64.URLEncoding.DecodeString(kv64); err == nil {
				kv := llog.KV{}
				if json.Unmarshal([]byte(kvStr), &kv) == nil {
					ctx = context.WithValue(ctx, ctxKVKey, kv)
				}
			}
		}

		// TODO actually set the context on the http request

		h.ServeHTTP(w, r)
	})
}

func applyRequestCtxHeaders(r *http.Request, ctx context.Context) {
	if kv := ContextKV(ctx); len(kv) > 0 {
		if kvB, err := json.Marshal(kv); err == nil {
			r.Header.Set(requestCtxKVHeader, base64.URLEncoding.EncodeToString(kvB))
		}
	}
}

// TODO ContextKV is kind of complex, would like to come up with a simpler
// system

// ContextKV returns the llog.KV embedded in the Context. If there isn't
// one, it attempts to get an *http.Request out of the Context and create a
// llog.KV based on that, and returns that. Otherwise, returns an empty llog.KV.
func ContextKV(ctx context.Context) llog.KV {
	kvi := ctx.Value(ctxKVKey)
	if kvi != nil {
		return kvi.(llog.KV).Copy()
	}
	r := lrpchttp.ContextRequest(ctx)
	if r == nil {
		return llog.KV{}
	}
	return rpcutil.RequestKV(r)
}

// ContextMergeKV returns a context with the given set of llog.KVs merged into
// it. Merging has the same behavior as the llog.Merge function
func ContextMergeKV(ctx context.Context, kvs ...llog.KV) context.Context {
	kv := ContextKV(ctx)
	kv = llog.Merge(kv, llog.Merge(kvs...))
	return context.WithValue(ctx, ctxKVKey, kv)
}
