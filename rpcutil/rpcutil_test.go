package rpcutil

import (
	"bufio"
	"errors"
	"io"
	"net/http"
	. "testing"

	"time"

	"github.com/gorilla/rpc/v2/json2"
	"github.com/levenlabs/go-llog"
	"github.com/levenlabs/golib/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestRPC struct{}

type TestArgs struct {
	Foo  int    `validate:"min=0"`
	Foo2 string `apply:"trim"`
}

type TestRes struct {
	Bar  int
	Bar2 string
}

func (rr TestRPC) DoFoo(r *http.Request, args *TestArgs, res *TestRes) error {
	if args.Foo == -1 {
		return &json2.Error{Code: 1, Message: "Foo can't be -1"}
	} else if args.Foo == -2 {
		return &json2.Error{Code: -1, Message: "Server doesn't know what -2 is"}
	} else if args.Foo == -3 {
		return errors.New("server can't even what")
	} else if args.Foo == -4 {
		return &QuietError{Code: 2, Message: "Don't log this"}
	}

	res.Bar = args.Foo
	res.Bar2 = args.Foo2
	return nil
}

func TestLLCodec(t *T) {
	llog.SetLevelFromString("WARN")
	oldOut := llog.Out

	r, w := io.Pipe()
	buf := bufio.NewReader(r)
	llog.Out = w
	defer func() {
		llog.Out = oldOut
	}()

	assertLog := func(severity, contains string) {
		doneCh := make(chan struct{})
		go func() {
			select {
			case <-time.After(2 * time.Second):
				assert.Fail(t, "timed out waiting for log %q:%q", severity, contains)
			case <-doneCh:
			}
		}()
		str, err := buf.ReadString('\n')
		require.Nil(t, err)
		assert.Contains(t, str, severity)
		assert.Contains(t, str, contains)
		close(doneCh)
	}

	c := NewLLCodec()
	h := JSONRPC2Handler(c, TestRPC{})

	// First test that normal, non-error calls work
	i := int(testutil.RandInt64())
	args := TestArgs{i, ""}
	res := TestRes{}
	require.Nil(t, JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args))
	assert.Equal(t, i, res.Bar)

	// Test that an application defined error makes it back to the user. This
	// should produce a WARN
	args = TestArgs{-1, ""}
	err := JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args)
	assert.Equal(t, &json2.Error{Code: 1, Message: "Foo can't be -1"}, err)
	assertLog("WARN", "Foo can't be -1")

	// Test that a server defined error makes it back to the user. This should
	// produce an ERROR
	args = TestArgs{-2, ""}
	err = JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args)
	assert.Equal(t, &json2.Error{
		Code: -1, Message: "Server doesn't know what -2 is",
	}, err)
	assertLog("ERROR", "jsonrpc internal server error")

	// Test that an unknown error makes it back to the user. This should produce
	// an ERROR
	args = TestArgs{-3, ""}
	err = JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args)
	assert.Equal(t, &json2.Error{
		Code: json2.E_SERVER, Message: "unexpected internal server error: server can't even what",
	}, err)
	assertLog("ERROR", "jsonrpc internal server error")

	// Now we disable showing internal server messages and repeat the last three
	// tests. The first should still return its error, the second two should
	// return a generic one. In all cases the same logs entries should be made
	c = NewLLCodec()
	c.HideServerErrors = true
	h = JSONRPC2Handler(c, TestRPC{})

	args = TestArgs{-1, ""}
	err = JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args)
	assert.Equal(t, &json2.Error{Code: 1, Message: "Foo can't be -1"}, err)
	assertLog("WARN", "Foo can't be -1")

	args = TestArgs{-2, ""}
	err = JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args)
	assert.Equal(t, &json2.Error{
		Code: json2.E_SERVER, Message: "internal server error",
	}, err)
	assertLog("ERROR", "jsonrpc internal server error")

	args = TestArgs{-3, ""}
	err = JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args)
	assert.Equal(t, &json2.Error{
		Code: json2.E_SERVER, Message: "internal server error",
	}, err)
	assertLog("ERROR", "jsonrpc internal server error")

	// Test validation, which ensures that Foo is >= 0
	c = NewLLCodec()
	c.ValidateInput = true
	h = JSONRPC2Handler(c, TestRPC{})

	// The normal test should still work
	i = int(testutil.RandInt64())
	args = TestArgs{i, ""}
	res = TestRes{}
	require.Nil(t, JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args))
	assert.Equal(t, i, res.Bar)

	// Anything under 0 should not validate
	args = TestArgs{-1, ""}
	err = JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args)
	assert.Equal(t, &json2.Error{
		Code: json2.E_BAD_PARAMS, Message: "Foo: less than min",
	}, err)
	assertLog("WARN", "Foo: less than min")

	// Test ResponseInliner, first returning nil to confirm that doesn't do
	// anything
	c = NewLLCodec()
	c.ResponseInliner = func(_ *http.Request) map[string]interface{} {
		return nil
	}
	h = JSONRPC2Handler(c, TestRPC{})

	// The normal test should not have changed
	i = int(testutil.RandInt64())
	args = TestArgs{i, ""}
	resm := map[string]interface{}{}
	require.Nil(t, JSONRPC2CallHandler(h, &resm, "TestRPC.DoFoo", &args))
	assert.Equal(t, 2, len(resm))
	assert.Equal(t, float64(i), resm["Bar"])

	// Now have it actually do something
	c.ResponseInliner = func(_ *http.Request) map[string]interface{} {
		return map[string]interface{}{
			"Extra": "turtles",
		}
	}
	h = JSONRPC2Handler(c, TestRPC{})

	i = int(testutil.RandInt64())
	args = TestArgs{i, ""}
	resm = map[string]interface{}{}
	require.Nil(t, JSONRPC2CallHandler(h, &resm, "TestRPC.DoFoo", &args))
	assert.Equal(t, 3, len(resm))
	assert.Equal(t, float64(i), resm["Bar"])
	assert.Equal(t, "turtles", resm["Extra"])

	// Test QuietErrors
	args = TestArgs{-4, ""}
	err = JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args)
	assert.Equal(t, &json2.Error{Code: 2, Message: "Don't log this"}, err)

	// Ghetto way of making sure the previous thing wasn't logged
	time.Sleep(100 * time.Millisecond)
	llog.Warn("test warning")
	assertLog("WARN", "test warning")

	// Test applicator, which ensures that Bar is trimmed
	c = NewLLCodec()
	c.RunInputApplicators = true
	h = JSONRPC2Handler(c, TestRPC{})

	// The normal call should not be trimmed
	s := "a"
	args = TestArgs{
		Foo2: " " + s + " ",
	}
	res = TestRes{}
	require.Nil(t, JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args))
	assert.Equal(t, s, res.Bar2)

	// Test applicator off
	c = NewLLCodec()
	h = JSONRPC2Handler(c, TestRPC{})

	// Input should not be trimmed
	s = " a "
	args = TestArgs{
		Foo2: s,
	}
	res = TestRes{}
	require.Nil(t, JSONRPC2CallHandler(h, &res, "TestRPC.DoFoo", &args))
	assert.Equal(t, s, res.Bar2)
}
