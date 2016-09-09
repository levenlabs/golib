package testutil

import (
	"fmt"
	"net/http"
)

// RoundTripper is an instance of a RoundTripper that allows you to pass
// the responses ahead of time per domain
type RoundTripper map[string]RoundTripperResponse

// Response is a response for a specific domain
type RoundTripperResponse struct {
	Resp *http.Response
	Err  error
}

// RoundTrip is needed to implement the RoundTripper interface
// It checks for an existing response for the domain first, and then falls back
// to looking for an existing response for the full uri
func (rt RoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, ok := rt[r.Host]
	if !ok {
		// try by request uri if host didn't match
		resp, ok = rt[r.URL.String()]
		if !ok {
			return nil, fmt.Errorf("invalid host sent: %s", r.Host)
		}
	}
	return resp.Resp, resp.Err
}
