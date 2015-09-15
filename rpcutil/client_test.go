package rpcutil

import (
	"net/http/httptest"
	. "testing"

	"github.com/gorilla/rpc/v2/json2"
	"github.com/levenlabs/golib/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONRPC2Call(t *T) {
	h := JSONRPC2Handler(json2.NewCodec(), TestRPC{})

	i := int(testutil.RandInt64())
	arg := TestArgs{i}
	res := TestRes{}
	require.Nil(t, JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &arg))
	assert.Equal(t, i, res.Bar)

	serv := httptest.NewServer(h)

	i = int(testutil.RandInt64())
	arg = TestArgs{i}
	res = TestRes{}
	require.Nil(t, JSONRPC2Call(serv.URL, &res, "TestRPC.DoFoo", &arg))
	assert.Equal(t, i, res.Bar)
}
