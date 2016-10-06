package genapi

import "net/http"

// AddCORSHeaders accepts a http.Handler and returns a http.Handler that adds
// CORS headers when an Origin is sent. No validation on the origin is done
func AddCORSHeaders(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Possibly check CORS and set the headers to send back if it matches
		origin := r.Header.Get("Origin")
		if origin != "" {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "DNT,Keep-Alive,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type")
		}
		// We allow OPTIONS so that preflighted requests can get CORS back
		if r.Method == "OPTIONS" {
			return
		}
		handler.ServeHTTP(w, r)
	})
}
