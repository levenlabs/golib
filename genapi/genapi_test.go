package genapi

import (
	"net"
	"net/http"
	"os"
	"syscall"
	. "testing"
	"time"

	"github.com/levenlabs/golib/rpcutil"
	"github.com/levenlabs/golib/testutil"
	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestInit(t *T) {
	i := 0
	var g *GenAPI
	g = &GenAPI{
		Init: func(g2 *GenAPI) {
			assert.Equal(t, g, g2)
			assert.Equal(t, 0, i)
			assert.Equal(t, TestMode, g2.Mode)
			i++
		},
	}
	g.AppendInit(func(g2 *GenAPI) {
		assert.Equal(t, g, g2)
		assert.Equal(t, 1, i)
		i++
	})
	g.AppendInit(func(g2 *GenAPI) {
		assert.Equal(t, g, g2)
		assert.Equal(t, 2, i)
		i++
	})
	g.TestMode()
	assert.Equal(t, 3, i)

	assert.Panics(t, func() {
		g.AppendInit(func(g2 *GenAPI) {})
	})
}

func TestSRVClientPreprocess(t *T) {
	dc := testutil.RandStr()
	os.Setenv("DATACENTER", dc)
	g := &GenAPI{}
	g.TestMode()
	dcHash := g.getDCHash()
	m := new(dns.Msg)
	m.Answer = []dns.RR{
		// The correct server with the local DC
		dns.RR(&dns.SRV{
			Target:   dcHash + "-" + testutil.RandStr(),
			Port:     uint16(80),
			Priority: uint16(5),
		}),
		// Verify priorty never comes back less than zero
		dns.RR(&dns.SRV{
			Target:   dcHash + "-" + testutil.RandStr(),
			Port:     uint16(80),
			Priority: uint16(0),
		}),
		// A server with a different DC
		dns.RR(&dns.SRV{
			Target:   testutil.RandStr() + "-" + testutil.RandStr(),
			Port:     uint16(80),
			Priority: uint16(5),
		}),
		// A server with no DC
		dns.RR(&dns.SRV{
			Target:   testutil.RandStr(),
			Port:     uint16(80),
			Priority: uint16(5),
		}),
		// Verify that a host hash that matches the DC hash doesn't get matched
		dns.RR(&dns.SRV{
			Target:   dcHash,
			Port:     uint16(80),
			Priority: uint16(5),
		}),
	}

	g.srvClientPreprocess(m)

	correctPri := []uint16{uint16(4), uint16(0), uint16(5), uint16(5), uint16(5)}
	for i := range m.Answer {
		ansSRV, ok := m.Answer[i].(*dns.SRV)
		require.True(t, ok)
		assert.Equal(t, ansSRV.Priority, correctPri[i])
	}
}

type APIModeTest struct{}

func (APIModeTest) Echo(r *http.Request, in, out *struct{ A int }) error {
	out.A = in.A
	return nil
}
func (APIModeTest) Sleep(r *http.Request, in, out *struct{ Time int }) error {
	time.Sleep(time.Duration(in.Time) * time.Second)
	out.Time = in.Time
	return nil
}

// Basic test to make sure listening is sane and requests work
func TestAPIMode(t *T) {
	ga := &GenAPI{
		Name:       "apimodetest",
		Services:   []interface{}{APIModeTest{}},
		InitDoneCh: make(chan bool),
	}

	go func() { ga.APIMode() }()
	<-ga.InitDoneCh

	assert.NotEmpty(t, ga.ListenAddr)

	var args, res struct{ A int }
	args.A = 2
	// ListenAddr is :port which doesn't have an IP so we just rip the port off
	// and make the call on localhost
	_, port, _ := net.SplitHostPort(ga.ListenAddr)
	err := rpcutil.JSONRPC2Call("http://127.0.0.1:"+port, &res, "APIModeTest.Echo", &args)
	require.Nil(t, err)
	assert.Equal(t, 2, res.A)

	resp, err := http.Get("http://127.0.0.1:" + port + "/health-check")
	require.Nil(t, err)
	assert.Equal(t, 200, resp.StatusCode)

}

func TestGracefulShutdown(t *T) {
	ga := &GenAPI{
		Name:       "apimodetest",
		Services:   []interface{}{APIModeTest{}},
		InitDoneCh: make(chan bool),
	}

	os.Setenv("APIMODETEST_UNHEALTHY_TIMEOUT", "3000")

	go func() { ga.APIMode() }()
	<-ga.InitDoneCh

	assert.NotEmpty(t, ga.ListenAddr)
	// ListenAddr is :port which doesn't have an IP so we just rip the port off
	// and make the call on localhost
	_, port, _ := net.SplitHostPort(ga.ListenAddr)

	// We want to test that health-check shows as unhealthy after a sigint is received
	ga.sigCh <- syscall.SIGINT
	time.Sleep(1000 * time.Millisecond)
	resp, err := http.Get("http://127.0.0.1:" + port + "/health-check")
	require.Nil(t, err)
	assert.Equal(t, 500, resp.StatusCode)

	// we should still be able to make a request, since it hasn't been 3 seconds
	// sleep for 5 seconds since definitely
	var args, res struct{ Time int }
	args.Time = 5
	err = rpcutil.JSONRPC2Call("http://127.0.0.1:"+port, &res, "APIModeTest.Sleep", &args)
	require.Nil(t, err)
	assert.Equal(t, 5, res.Time)

	// now all the calls should fail
	err = rpcutil.JSONRPC2Call("http://127.0.0.1:"+port, &res, "APIModeTest.Sleep", &args)
	assert.NotNil(t, err)
}
