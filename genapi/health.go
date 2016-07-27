package genapi

import (
	"fmt"
	"net/http"

	"github.com/levenlabs/go-llog"
)

// Healther is an interface that any entity can implement which will report back
// whether or not that entity is "healthy". An unhealthy entity is, in effect,
// saying that it could potentially do it's job but at the moment is should not
// be relied on to do so
type Healther interface {
	Healthy() error
}

// sneezy is never healthy and always returns an error when Healthy is called on it.
type sneezy struct{}

// Healthy always returns an error for type sneezy.
func (s sneezy) Healthy() error {
	return fmt.Errorf("Achoo! I'm Sneezy!")
}

func (g *GenAPI) healthCheck() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.healthersL.Lock()
		defer g.healthersL.Unlock()
		llog.Debug("serving /health-check")
		for name, h := range g.Healthers {
			if err := h.Healthy(); err != nil {
				llog.Error("health check failed", llog.KV{
					"name": name,
					"err":  err,
				})
				http.Error(w, "Not healthy! :(", http.StatusInternalServerError)
				return
			}
		}
	})
}
