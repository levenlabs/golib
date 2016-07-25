package genapi

import (
	"net"
	"net/http"
	"net/http/pprof"
	"runtime"
	"time"

	"github.com/levenlabs/go-llog"
)

// TODO test this noise

// Handler is what is returned by HandlerTpl
type Handler struct {
	http.Handler
	hw *httpWaiter
}

// Wait will block until all active requests have completed. This does not stop
// incoming requests in any way
func (h Handler) Wait() {
	h.hw.wait()
}

// HandlerTpl describes information needed to create an http.Handler which wraps
// a ServeMux with additional parameters
type HandlerTpl struct {
	// The http.Handler which will be assigned to the "/" endpoint in the
	// underlying mux.
	RootHandler http.Handler

	// Optional set of Healthers which should be checked during a /health-check.
	// These will be checked sequentially, and if any return an error that will
	// be logged and the health check will return false. The key in the map is a
	// name for the Healther which can be logged
	Healthers map[string]Healther

	// Optional, defaults to the return of os.Hostname, used to populate the
	// X-Hostname header on all responses
	Hostname string
}

// Handler returns the an instance of http.Handler based on the template's
// fields
func (htpl HandlerTpl) Handler() (http.Handler, error) {
	m := http.NewServeMux()

	// The net/http/pprof package expects to be under /debug/pprof/, which is
	// why we don't strip the prefix here
	m.Handle("/debug/pprof/", pprofHandler())

	if htpl.Healthers != nil {
		m.Handle("/health-check", healthCheckHandler(htpl.Healthers))
	}

	if htpl.RootHandler != nil {
		m.Handle("/", htpl.RootHandler)
	}

	// Base handler is the mux
	var h Handler
	h.Handler = m

	// http waiter
	h.hw = newHTTPWaiter(h.Handler)
	h.Handler = h.hw

	// counter
	ch := &countHandler{Handler: h.Handler}
	go func() {
		for range time.Tick(1 * time.Minute) {
			c := ch.Count()
			llog.Info("count requests in last minute", llog.KV{"count": c})
		}
	}()
	h.Handler = ch

	// context TODO might not actually be needed at all
	h.Handler = contextHandler(h.Handler)

	return h, nil
}

// Healther is an interface that any entity can implement which will report back
// whether or not that entity is "healthy". An unhealthy entity is, in effect,
// saying that it could potentially do it's job but at the moment is should not
// be relied on to do so
type Healther interface {
	Healthy() error
}

func healthCheckHandler(healthers map[string]Healther) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llog.Debug("serving /health-check")
		for name, healther := range healthers {
			if err := healther.Healthy(); err != nil {
				llog.Error("health check failed", llog.KV{
					"name": name,
					"err":  err,
				})
				http.Error(w, "Not healthy! :(", http.StatusInternalServerError)
			}
		}
	})
}

func pprofHandler() http.Handler {
	h := http.NewServeMux()
	h.Handle("/debug/pprof/", http.HandlerFunc(pprof.Index))

	// Even though Index handles this, this particular one won't work without
	// setting the BlockProfileRate temporarily.
	h.HandleFunc("/debug/pprof/block", func(w http.ResponseWriter, r *http.Request) {
		runtime.SetBlockProfileRate(1)
		time.Sleep(5 * time.Second)
		pprof.Index(w, r)
		runtime.SetBlockProfileRate(0)
	})

	h.Handle("/debug/pprof/cmdline", http.HandlerFunc(pprof.Cmdline))
	h.Handle("/debug/pprof/profile", http.HandlerFunc(pprof.Profile))
	h.Handle("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol))
	h.Handle("/debug/pprof/trace", http.HandlerFunc(pprof.Trace))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ipStr, _, _ := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(ipStr)
		if ip == nil || !ip.IsLoopback() {
			http.Error(w, "", 403) // forbidden
			return
		}
		h.ServeHTTP(w, r)
	})
}
