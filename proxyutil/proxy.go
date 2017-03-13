// Package proxyutil implements helper types and functions for proxying
// (generally non-rpc) http requests to/from backing services.
package proxyutil

import (
	"compress/flate"
	"compress/gzip"
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

// DisableCompression disables the built-in Transport's decompression.
// It seems like the built-in decompression is only done when no
// Accept-Encoding header was sent. If you want to remove uncertainty, call
// this. If you're not reading the body at all, you should call this as an
// optimization.
// Additionally: https://github.com/golang/go/issues/18779
// If you call this, you must call DecodeResponse, before reading the
// response's body from Do().
func (rp ReverseProxyClient) DisableCompression() {
	// make sure a client is set, and if not, set it to default
	if rp.Client == nil {
		rp.Client = http.DefaultClient
	}
	if t, ok := rp.Client.Transport.(*http.Transport); ok {
		t.DisableCompression = true
	}
}

// TODO this is somewhat busterino. req is passed in with its URL changed, but
// implicitly its RemoteAddr is still the original, and is then used to fill in
// X-Forwarded-For. That's already hacky. In addition if we want to set
// X-Forwarded-Proto we'd _have_ to have the original URL. So this whole thing
// needs refactoring

// Do will perform the given request, which should have been taken in from an
// http.Handler and is now being forwarded on with a new URL set. If the
// request's context is cancelled then the request will be cancelled.
//
// If you expect an encoded (compressed) response, use DecodeResponse to decode
// it before reading. If you're just piping this to WriteResponse, you don't
// need DecodeResponse.
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
	// we cannot make a request with a RequestURI
	outreq.RequestURI = ""

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
	// now clear the RemoteAddr
	outreq.RemoteAddr = ""

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

// WriteResponse writes the given response into the ResponseWriter. It will
// encode the Response's Body according to its Content-Encoding header, if it
// has one.
func WriteResponse(dst http.ResponseWriter, src *http.Response) error {
	head := http.Header{}
	copyHeader(head, src.Header)
	// remove any hop-by-hop headers we might've just set
	for _, h := range hopHeaders {
		head.Del(h)
	}
	copyHeader(dst.Header(), head)

	// combine and de-dup side-by-side hosts
	mh := map[string][]string(dst.Header())
	if hs := mh["X-Hostname"]; len(hs) > 1 {
		var all []string
		for _, v := range hs {
			all = append(all, strings.Split(v, ",")...)
		}
		var val string
		for i, v := range all {
			if i == 0 {
				val = v
				continue
			}
			if all[i-1] == all[i] {
				continue
			}
			val += "," + v
		}
		dst.Header().Set("X-Hostname", val)
	}

	// The "Trailer" header isn't included in the Transport's response,
	// at least for *http.Transport. Build it up from Trailer.
	if len(src.Trailer) > 0 {
		var trailerKeys []string
		for k := range src.Trailer {
			trailerKeys = append(trailerKeys, k)
		}
		dst.Header().Add("Trailer", strings.Join(trailerKeys, ", "))
	}

	var dstW io.WriteCloser
	// if the response is still compressed then don't try to re-compress it
	if src.Uncompressed {
		switch src.Header.Get("Content-Encoding") {
		case "gzip":
			dstW = gzip.NewWriter(dst)
			dst.Header().Del("Content-Length")
		case "deflate":
			// From the docs: If level is in the range [-1, 9] then the error
			// returned will be nil. Otherwise the error returned will be non-nil
			// so we can ignore error
			dstW, _ = flate.NewWriter(dst, -1)
			dst.Header().Del("Content-Length")
		}
	}
	// fallback to nop if we're not encoding anything
	if dstW == nil {
		dstW = nopWriteCloser{dst} // defined in brw.go
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

	var err error
	if _, err = io.Copy(dstW, src.Body); err != nil {
		// bail, gotta close src.Body first though
	} else if err = dstW.Close(); err != nil {
		// bail, gotta close src.Body first though
	}

	src.Body.Close() // close now, instead of defer, to populate src.Trailer
	if err != nil {
		return err
	}

	copyHeader(dst.Header(), src.Trailer)
	return nil
}

// readCloserComposition is a composition of ReadClosers, Read is always just
// called on the first one, but close is propagated to all of them
type readCloserComposition []io.ReadCloser

// Read implements the io.Reader interface
func (rs readCloserComposition) Read(p []byte) (int, error) {
	return rs[0].Read(p)
}

// Close implements the io.Closer interface
// it returns the last non-nil error received
func (rs readCloserComposition) Close() error {
	var err error
	for i := range rs {
		if e := rs[i].Close(); e != nil {
			err = e
		}
	}
	return err
}

// DecodeResponse takes a http.Response from Do() and replaces the body with an
// uncompressed form. It also sets resp.Uncompressed to true. If the body is
// already decompressed (because you didn't use Do()) then, this does nothing.
func DecodeResponse(resp *http.Response) error {
	if resp.Uncompressed {
		return nil
	}
	var rc io.ReadCloser
	var err error
	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		rc, err = gzip.NewReader(resp.Body)
		if err != nil {
			return err
		}
	case "deflate":
		rc = flate.NewReader(resp.Body)
	default:
		return nil
	}
	// we need the Composition to propagate the close to both of them
	resp.Body = readCloserComposition([]io.ReadCloser{
		rc,
		resp.Body,
	})
	resp.Uncompressed = true
	return nil
}
