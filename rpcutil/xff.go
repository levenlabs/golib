package rpcutil

import (
	"net/http"

	"github.com/levenlabs/golib/proxyutil"
)

// RequestIP is deprecated, and has been moved to proxyutil
func RequestIP(r *http.Request) string {
	return proxyutil.RequestIP(r)
}

// AddProxyXForwardedFor is deprecated, and has been moved to proxyutil
func AddProxyXForwardedFor(dst, src *http.Request) {
	proxyutil.AddXForwardedFor(dst, src)
}
