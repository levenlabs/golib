package rpcutil

import (
	"errors"
	"net/http"
	. "testing"

	"github.com/gorilla/rpc/v2/json2"
	"github.com/levenlabs/golib/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestRPC struct{}

type TestArgs struct {
	Foo int `validate:"min=0"`
}

type TestRes struct {
	Bar int
}

func (rr TestRPC) DoFoo(r *http.Request, args *TestArgs, res *TestRes) error {
	if args.Foo == -1 {
		return &json2.Error{Code: 1, Message: "Foo can't be -1"}
	} else if args.Foo == -2 {
		return &json2.Error{Code: -1, Message: "Server doesn't know what -2 is"}
	} else if args.Foo == -3 {
		return errors.New("server can't even what")
	}

	res.Bar = args.Foo
	return nil
}

func TestLLCodec(t *T) {
	c := NewLLCodec()
	h := JSONRPC2Handler(c, TestRPC{})

	// First test that normal, non-error calls work
	i := int(testutil.RandInt64())
	args := TestArgs{i}
	res := TestRes{}
	require.Nil(t, JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args))
	assert.Equal(t, i, res.Bar)

	// Test that an application defined error makes it back to the user. This
	// should produce a WARN
	args = TestArgs{-1}
	err := JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args)
	assert.Equal(t, &json2.Error{Code: 1, Message: "Foo can't be -1"}, err)

	// Test that a server defined error makes it back to the user. This should
	// produce an ERROR
	args = TestArgs{-2}
	err = JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args)
	assert.Equal(t, &json2.Error{
		Code: -1, Message: "Server doesn't know what -2 is",
	}, err)

	// Test that an unknown error makes it back to the user. This should produce
	// an ERROR
	args = TestArgs{-3}
	err = JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args)
	assert.Equal(t, &json2.Error{
		Code: json2.E_SERVER, Message: "unexpected internal server error: server can't even what",
	}, err)

	// Now we disable showing internal server messages and repeat the last three
	// tests. The first should still return its error, the second two should
	// return a generic one. In all cases the same logs entries should be made
	c = NewLLCodec()
	c.HideServerErrors = true
	h = JSONRPC2Handler(c, TestRPC{})

	args = TestArgs{-1}
	err = JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args)
	assert.Equal(t, &json2.Error{Code: 1, Message: "Foo can't be -1"}, err)

	args = TestArgs{-2}
	err = JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args)
	assert.Equal(t, &json2.Error{
		Code: json2.E_SERVER, Message: "internal server error",
	}, err)

	args = TestArgs{-3}
	err = JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args)
	assert.Equal(t, &json2.Error{
		Code: json2.E_SERVER, Message: "internal server error",
	}, err)

	// Test validation, which ensures that Foo is >= 0
	c = NewLLCodec()
	c.ValidateInput = true
	h = JSONRPC2Handler(c, TestRPC{})

	// The normal test should still work
	i = int(testutil.RandInt64())
	args = TestArgs{i}
	res = TestRes{}
	require.Nil(t, JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args))
	assert.Equal(t, i, res.Bar)

	// Anything under 0 should not validate
	args = TestArgs{-1}
	err = JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args)
	assert.Equal(t, &json2.Error{
		Code: json2.E_BAD_PARAMS, Message: "Foo: less than min",
	}, err)

	// Test ResponseInliner, first returning nil to confirm that doesn't do
	// anything
	c = NewLLCodec()
	c.ResponseInliner = func(_ *http.Request) map[string]interface{} {
		return nil
	}
	h = JSONRPC2Handler(c, TestRPC{})

	// The normal test should not have changed
	i = int(testutil.RandInt64())
	args = TestArgs{i}
	resm := map[string]interface{}{}
	require.Nil(t, JSONRPC2CallHandler(h, &resm, "TestRPC.DoFoo", &args))
	assert.Equal(t, 1, len(resm))
	assert.Equal(t, float64(i), resm["Bar"])

	// Now have it actually do something
	c.ResponseInliner = func(_ *http.Request) map[string]interface{} {
		return map[string]interface{}{
			"Extra": "turtles",
		}
	}
	h = JSONRPC2Handler(c, TestRPC{})

	i = int(testutil.RandInt64())
	args = TestArgs{i}
	resm = map[string]interface{}{}
	require.Nil(t, JSONRPC2CallHandler(h, &resm, "TestRPC.DoFoo", &args))
	assert.Equal(t, 2, len(resm))
	assert.Equal(t, float64(i), resm["Bar"])
	assert.Equal(t, "turtles", resm["Extra"])
}
