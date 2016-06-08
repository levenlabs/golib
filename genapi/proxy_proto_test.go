package genapi

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	. "testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyProto(t *T) {
	l, err := net.Listen("tcp", ":0") // any port
	require.Nil(t, err)

	pl, err := newProxyListener(l, []string{"127.0.0.1/32"})
	require.Nil(t, err)

	_, portStr, err := net.SplitHostPort(pl.Addr().String())
	require.Nil(t, err)

	assertReadAll := func(expect string, in io.Reader) {
		b, err := ioutil.ReadAll(in)
		require.Nil(t, err)
		assert.Equal(t, expect, string(b))
	}

	proxyLine := fmt.Sprintf("PROXY TCP4 8.8.8.8 127.0.0.1 44444 %s\r\n", portStr)

	// Make sure an allowed connection can use the PROXY protocol
	go func() {
		c, err := net.Dial("tcp", "127.0.0.1:"+portStr)
		require.Nil(t, err)

		fmt.Fprint(c, proxyLine)
		fmt.Fprint(c, "hi")
		c.Close()
	}()
	c, err := pl.Accept()
	require.Nil(t, err)
	assert.Equal(t, "8.8.8.8:44444", c.RemoteAddr().String())
	assertReadAll("hi", c)

	// Make sure an allowed connection which doesn't use the PROXY protocol also
	// works
	go func() {
		c, err := net.Dial("tcp", "127.0.0.1:"+portStr)
		require.Nil(t, err)
		fmt.Fprint(c, "hi")
		c.Close()
	}()
	c, err = pl.Accept()
	require.Nil(t, err)
	ipStr, _, err := net.SplitHostPort(c.RemoteAddr().String())
	require.Nil(t, err)
	assert.Equal(t, "127.0.0.1", ipStr)
	assertReadAll("hi", c)

	// Make sure a disallowed connection can't
	go func() {
		c, err := net.Dial("tcp", "[::1]:"+portStr)
		require.Nil(t, err)

		fmt.Fprint(c, proxyLine)
		fmt.Fprint(c, "hi")
		c.Close()
	}()
	c, err = pl.Accept()
	require.Nil(t, err)
	ipStr, _, err = net.SplitHostPort(c.RemoteAddr().String())
	require.Nil(t, err)
	assert.Equal(t, "::1", ipStr)
	assertReadAll(proxyLine+"hi", c)
}
