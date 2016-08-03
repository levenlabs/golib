// Package proxyutil implements helper types and functions for proxying
// (generally non-rpc) http requests to/from backing services.
package proxyutil

import (
	"encoding/gob"
	"io"
	"net/http"
	"strings"
)

// The logic in this file is largely lifted from the implementation of
// httputil.ReverseProxy

func init() {
	gob.Register(new(brwMarshalled))
}

// ReverseProxyClient is a wrapper around a basic *http.Client which performs
// requests as a reverse http proxy instead of a normal client. This involves
// adding/removing certain headers and dealing with Keep-Alive.
type ReverseProxyClient struct {
	// If nil then http.DefaultClient will beused
	Client *http.Client
}

// Hop-by-hop headers. These are removed when sent to the backend.
// http://www.w3.org/Protocols/rfc2616/rfc2616-sec13.html
var hopHeaders = []string{
	"Connection",
	"Proxy-Connection", // non-standard but still sent by libcurl and rejected by e.g. google
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",      // canonicalized version of "TE"
	"Trailer", // not Trailers per URL above; http://www.rfc-editor.org/errata_search.php?eid=4522
	"Transfer-Encoding",
	"Upgrade",
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// Do will perform the given request, which should have been taken in from an
// http.Handler and is now being forwarded on with a new URL set. If the
// request's context is cancelled then the request will be cancelled.
func (rp ReverseProxyClient) Do(req *http.Request) (*http.Response, error) {
	cl := rp.Client
	if cl == nil {
		cl = http.DefaultClient
	}

	outreq := new(http.Request)
	*outreq = *req // includes shallow copies of maps, but okay

	outreq.Proto = "HTTP/1.1"
	outreq.ProtoMajor = 1
	outreq.ProtoMinor = 1
	outreq.Close = false

	// Remove hop-by-hop headers to the backend. Especially
	// important is "Connection" because we want a persistent
	// connection, regardless of what the client sent to us. This
	// is modifying the same underlying map from req (shallow
	// copied above) so we only copy it if necessary.
	copiedHeaders := false
	for _, h := range hopHeaders {
		if outreq.Header.Get(h) != "" {
			if !copiedHeaders {
				outreq.Header = make(http.Header)
				copyHeader(outreq.Header, req.Header)
				copiedHeaders = true
			}
			outreq.Header.Del(h)
		}
	}

	AddXForwardedFor(outreq, req)

	res, err := cl.Do(outreq)
	if err != nil {
		return nil, err
	}

	for _, h := range hopHeaders {
		res.Header.Del(h)
	}

	return res, nil
}

// FilterEncodings adjusts the Accept-Encoding header, if set, so that it only
// allows at most the given encodings. If it previously had a subset of the
// given encodings only those will be left. At least one encoding must be passed
// in.
func FilterEncodings(r *http.Request, encodings ...string) {
	ae := r.Header.Get("Accept-Encoding")
	if ae == "*" {
		r.Header.Set("Accept-Encoding", strings.Join(encodings, ", "))
	} else if ae == "" {
		return
	}

	encAllowed := func(e string) bool {
		for _, ee := range encodings {
			if e == ee {
				return true
			}
		}
		return false
	}

	aep := strings.Split(ae, ",")
	newaep := make([]string, 0, len(aep))
	for _, e := range aep {
		// since were splitting on , there might be a space before
		e = strings.TrimSpace(e)
		// according to the spec, there also might be a q value after a
		// semicolon, we don't really care about the q ourselves so ignore
		if encAllowed(strings.Split(e, ";")[0]) {
			// send the original q value since that means something special
			newaep = append(newaep, e)
		}
	}
	r.Header.Set("Accept-Encoding", strings.Join(newaep, ", "))
}

// WriteResponse writes the given response into the ResponseWriter
func WriteResponse(dst http.ResponseWriter, src *http.Response) {
	copyHeader(dst.Header(), src.Header)

	// The "Trailer" header isn't included in the Transport's response,
	// at least for *http.Transport. Build it up from Trailer.
	if len(src.Trailer) > 0 {
		var trailerKeys []string
		for k := range src.Trailer {
			trailerKeys = append(trailerKeys, k)
		}
		dst.Header().Add("Trailer", strings.Join(trailerKeys, ", "))
	}

	dst.WriteHeader(src.StatusCode)
	if len(src.Trailer) > 0 {
		// Force chunking if we saw a response trailer.
		// This prevents net/http from calculating the length for short
		// bodies and adding a Content-Length.
		if fl, ok := dst.(http.Flusher); ok {
			fl.Flush()
		}
	}

	io.Copy(dst, src.Body)
	src.Body.Close() // close now, instead of defer, to populate src.Trailer
	copyHeader(dst.Header(), src.Trailer)
}