package genapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/levenlabs/go-llog"

	"golang.org/x/net/context"
)

const (
	requestCtxKVHeader = "X-LL-CTX-KV"
)

type ctxKey int

const ctxKVKey ctxKey = 0

// TODO contextHandler, requestKV and applyRequestCtxHeaders are LL specific

func requestKV(r *http.Request) llog.KV {
	kv := llog.KV{
		"ip": RequestIP(r),
	}
	// first try Referer, but fallback to Origin
	if ref := r.Header.Get("Referer"); ref != "" {
		kv["referer"] = ref
	} else if o := r.Header.Get("Origin"); o != "" {
		kv["origin"] = o
	}
	if via := r.Header.Get("Via"); via != "" {
		kv["via"] = via
	}
	return kv
}

func contextHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if kv64 := r.Header.Get(requestCtxKVHeader); kv64 != "" {
			if kvStr, err := base64.URLEncoding.DecodeString(kv64); err == nil {
				kv := llog.KV{}
				if json.Unmarshal([]byte(kvStr), &kv) == nil {
					ctx = ContextMergeKV(ctx, requestKV(r), kv)
				}
			}
		}

		r = r.WithContext(ctx)

		h.ServeHTTP(w, r)
	})
}

// TODO this logic should go in LLRPCCaller?
func applyRequestCtxHeaders(r *http.Request, ctx context.Context) {
	if kv := ContextKV(ctx); len(kv) > 0 {
		if kvB, err := json.Marshal(kv); err == nil {
			r.Header.Set(requestCtxKVHeader, base64.URLEncoding.EncodeToString(kvB))
		}
	}
}

// TODO ContextKV is kind of complex, would like to come up with a simpler
// system

// ContextKV returns the llog.KV embedded in the Context. Otherwise, returns an
// empty llog.KV.
func ContextKV(ctx context.Context) llog.KV {
	kvi := ctx.Value(ctxKVKey)
	if kvi != nil {
		return kvi.(llog.KV).Copy()
	}
	return llog.KV{}
}

// ContextMergeKV returns a context with the given set of llog.KVs merged into
// it. Merging has the same behavior as the llog.Merge function
func ContextMergeKV(ctx context.Context, kvs ...llog.KV) context.Context {
	kv := ContextKV(ctx)
	kv = llog.Merge(kv, llog.Merge(kvs...))
	return context.WithValue(ctx, ctxKVKey, kv)
}
