package genapi

import (
	. "testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var testWaiter = newRPCWaiter()
var testWaiterHandler = func() lrpc.Handler {
	m := lrpc.NewServeMux()

	m.HandleFunc("default", func(lrpc.Call) {
		return "OHAI"
	})

	m.HandleFunc("sleep", func(lrpc.Call) {
		time.Sleep(1 * time.Second)
		return "OHAI"
	})

	m.HandleFunc("panic", func(lrpc.Call) {
		panic("oh snap")
	})

	return testWaiter.handler(m)
}()

func TestRPCWaiter(t *T) {
	for _, u := range []string{"default", "sleep", "panic"} {
		go func(u string) {

			var panicked bool
			defer func() {
				if recover() != nil {
					panicked = true
					assert.Equal(t, "panic", u)
				}
			}()

			ret := testWaiterHandler.ServeRPC(lrpc.DirectCall{Method: u})
			if u == "panic" {
				assert.True(panicked)
			} else {
				assert.Equal(t, "OHAI", ret.(string))
			}
		}(u)
	}

	start := time.Now()
	// give the goroutines time to actually make their requests
	time.Sleep(100 * time.Millisecond)
	go func() {
		time.Sleep(2 * time.Second)
		t.Fatal("timeout waiting for rpcWaiter")
	}()

	testWaiter.wait()
	since := time.Since(start)
	t.Logf("since: %v", since)
	assert.True(t, time.Since(start) > 1*time.Second)
}
