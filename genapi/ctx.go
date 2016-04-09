package genapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/levenlabs/go-llog"
	"github.com/levenlabs/golib/timeutil"
	"golang.org/x/net/context"
)

const (
	requestCtxKVHeader       = "X-LL-CTX-KV"
	requestCtxDeadlineHeader = "X-LL-CTX-DEADLINE"
)

type ctxKey int

const ctxKVKey ctxKey = 0

func requestCtx(r *http.Request) context.Context {
	ctx := context.Background()

	if kv64 := r.Header.Get(requestCtxKVHeader); kv64 != "" {
		if kvStr, err := base64.URLEncoding.DecodeString(kv64); err == nil {
			kv := llog.KV{}
			if json.Unmarshal([]byte(kvStr), &kv) == nil {
				ctx = context.WithValue(ctx, ctxKVKey, kv)
			}
		}
	}

	if deadlineStr := r.Header.Get(requestCtxDeadlineHeader); deadlineStr != "" {
		if deadlineF, err := strconv.ParseFloat(deadlineStr, 64); err == nil {
			deadline := timeutil.TimestampFromFloat64(deadlineF)
			ctx, _ = context.WithDeadline(ctx, deadline.Time)
		}
	}

	return ctx
}

// ContextKV returns the llog.KV associated with the given context, which was
// presumably returned from RequestContext. If no llog.KV is on the context, and
// empty llog.KV is returned
func ContextKV(ctx context.Context) llog.KV {
	if kvi := ctx.Value(ctxKVKey); kvi == nil {
		return llog.KV{}
	} else if kv, ok := kvi.(llog.KV); !ok {
		return llog.KV{}
	} else {
		return kv
	}
}

// ContextMergeKV returns a context with the given set of llog.KVs merged into
// it. Merging has the same behavior as the llog.Merge function
func ContextMergeKV(ctx context.Context, kvs ...llog.KV) context.Context {
	kv := ContextKV(ctx)
	kv = llog.Merge(kv, llog.Merge(kvs...))
	return context.WithValue(ctx, ctxKVKey, kv)
}

// ContextApply takes the context and adds information from it to the given
// Request, so that if the Request is sent to another genapi the Context will be
// propogated
func ContextApply(r *http.Request, ctx context.Context) {
	if kv := ContextKV(ctx); len(kv) > 0 {
		if kvB, err := json.Marshal(kv); err == nil {
			r.Header.Set(requestCtxKVHeader, base64.URLEncoding.EncodeToString(kvB))
		}
	}

	if deadline, ok := ctx.Deadline(); ok {
		r.Header.Set(requestCtxDeadlineHeader, timeutil.Timestamp{deadline}.String())
	}
}
