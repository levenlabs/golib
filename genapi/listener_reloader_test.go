package genapi

import (
	"bufio"
	"fmt"
	"io"
	"net"
	. "testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// simple middleware for testing. all accepted connections have the prefix
// immediately written to them
type prefixerListener struct {
	net.Listener
	prefix string
}

func (pl prefixerListener) Accept() (net.Conn, error) {
	c, err := pl.Listener.Accept()
	if err != nil {
		return c, err
	}

	fmt.Fprint(c, pl.prefix)
	return c, nil
}

func TestListenerReloader(t *T) {
	l, err := net.Listen("tcp", ":0") // any port
	require.Nil(t, err)

	assertReadLine := func(expect string, in io.Reader) {
		b, err := bufio.NewReader(in).ReadString('\n')
		require.Nil(t, err)
		assert.Equal(t, expect, string(b))
	}

	prefix := "foo\n"
	maker := func(ll net.Listener) (net.Listener, error) {
		return prefixerListener{ll, prefix}, nil
	}
	lr, err := newListenerReloader(l, maker)
	require.Nil(t, err)
	_, portStr, err := net.SplitHostPort(l.Addr().String())
	require.Nil(t, err)

	go func() {
		for {
			c, err := lr.Accept()
			require.Nil(t, err)
			c.Read(make([]byte, 1)) // block till close
		}
	}()

	c, err := net.Dial("tcp", "127.0.0.1:"+portStr)
	require.Nil(t, err)
	assertReadLine("foo\n", c)

	// swap the listener before closing that connection, so that when Accept is
	// called next it will be on the new "bar" listener
	prefix = "bar\n"
	require.Nil(t, lr.Reload())

	c.Close()

	c, err = net.Dial("tcp", "127.0.0.1:"+portStr)
	require.Nil(t, err)
	assertReadLine("bar\n", c)
	c.Close()
}

func TestListenerReloaderClose(t *T) {
	l, err := net.Listen("tcp", ":0") // any port
	require.Nil(t, err)

	maker := func(ll net.Listener) (net.Listener, error) {
		return prefixerListener{ll, "foo\n"}, nil
	}
	lr, err := newListenerReloader(l, maker)
	require.Nil(t, err)

	ch := make(chan error)
	// we do 2 at once to test for races
	go func() {
		ch <- lr.Close()
	}()
	go func() {
		ch <- lr.Close()
	}()

	// since we don't know how long Close() takes on a listener, the ordering is
	// unknown here, but one should be nil and one should not be
	err1 := <-ch
	err2 := <-ch
	assert.NotEqual(t, err1, err2)
	assert.True(t, err1 == nil || err2 == nil)
	assert.True(t, err1 != nil || err2 != nil)
}
