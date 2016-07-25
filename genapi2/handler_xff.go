package genapi

import (
	"net"
	"net/http"

	"github.com/levenlabs/golib/rpcutil"
)

func xffHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer h.ServeHTTP(w, r)

		_, portStr, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return
		}

		r.RemoteAddr = rpcutil.RequestIP(r) + ":" + portStr
	})
}

// TODO when writing call stuff, remember to add X-Forwarded-For
