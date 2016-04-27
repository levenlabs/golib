package genapi

import (
	. "testing"

	"github.com/levenlabs/golib/testutil"
	"github.com/stretchr/testify/assert"
)

func TestCallerStub(t *T) {
	method := "Test.Test"
	args := map[string]interface{}{
		testutil.RandStr(): testutil.RandStr(),
		testutil.RandStr(): testutil.RandStr(),
	}

	stub := CallerStub(func(method string, args interface{}) (interface{}, error) {
		return map[string]interface{}{
			"method": method,
			"args":   args,
		}, nil
	})
	res1 := map[string]interface{}{}
	assert.Nil(t, stub.Call(nil, &res1, method, args))
	assert.Equal(t, map[string]interface{}{
		"method": method,
		"args":   args,
	}, res1)

	type testType struct {
		method string
		args   map[string]interface{}
	}

	stub = CallerStub(func(method string, args interface{}) (interface{}, error) {
		return testType{method, args.(map[string]interface{})}, nil
	})
	res2 := testType{}
	assert.Nil(t, stub.Call(nil, &res2, method, args))
	assert.Equal(t, testType{method, args}, res2)

	assert.Nil(t, stub.Call(nil, nil, method, args))

	stub = CallerStub(func(method string, args interface{}) (interface{}, error) {
		return nil, nil
	})
	assert.Nil(t, stub.Call(nil, nil, method, args))
}
