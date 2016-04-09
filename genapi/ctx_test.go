package genapi

import (
	"encoding/base64"
	"net/http"
	. "testing"

	"golang.org/x/net/context"

	"github.com/levenlabs/go-llog"
	"github.com/levenlabs/golib/timeutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestCtx(t *T) {
	r, err := http.NewRequest("GET", "http://localhost", nil)
	require.Nil(t, err)

	ctx := requestCtx(r)
	assert.Empty(t, ContextKV(ctx))
	_, ok := ctx.Deadline()
	assert.False(t, ok)

	kv64 := base64.URLEncoding.EncodeToString([]byte(`{"foo":"bar"}`))
	r.Header.Set(requestCtxKVHeader, kv64)
	ctx = requestCtx(r)
	assert.Equal(t, llog.KV{"foo": "bar"}, ContextKV(ctx))
	_, ok = ctx.Deadline()
	assert.False(t, ok)

	now := timeutil.TimestampNow()
	r.Header.Set(requestCtxDeadlineHeader, now.String())
	ctx = requestCtx(r)
	assert.Equal(t, llog.KV{"foo": "bar"}, ContextKV(ctx))
	deadline, ok := ctx.Deadline()
	assert.True(t, ok)
	assert.Equal(t, now.Time, deadline)
}

func TestContextMergeKV(t *T) {
	ctx := context.Background()
	assert.Empty(t, ContextKV(ctx))

	ctx = ContextMergeKV(ctx, llog.KV{"foo": "a"})
	assert.Equal(t, llog.KV{"foo": "a"}, ContextKV(ctx))

	ctx = ContextMergeKV(ctx, llog.KV{"foo": "aaa"}, llog.KV{"foo": "a", "bar": "b"})
	assert.Equal(t, llog.KV{"foo": "a", "bar": "b"}, ContextKV(ctx))
}
