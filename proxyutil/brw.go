package proxyutil

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"encoding/gob"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
)

// BufferedResponseWriterEncodings is the set of encodings that
// BufferedResponseWriter supports
var BufferedResponseWriterEncodings = []string{"gzip", "deflate"}

// BufferedResponseWriter is a wrapper around a real ResponseWriter which
// actually writes all data to a buffer instead of the ResponseWriter. It also
// catches calls to WriteHeader. Once writing is done, GetBody can be called to
// get the buffered body, and SetBody can be called to set a new body to be
// used.  When ActuallyWrite is called all headers will be written to the
// ResponseWriter (with corrected Content-Length if SetBody was called),
// followed by the body.
//
// BufferedResponseWriter will transparently handle uncompressing and
// recompressing the response body when it sees a known Content-Encoding.
type BufferedResponseWriter struct {
	http.ResponseWriter
	buffer  *bytes.Buffer
	newBody io.Reader

	Code int
}

// NewBufferedResponseWriter returns an initialized BufferedResponseWriter,
// which will catch writes going to the given ResponseWriter
func NewBufferedResponseWriter(rw http.ResponseWriter) *BufferedResponseWriter {
	return &BufferedResponseWriter{
		ResponseWriter: rw,
		buffer:         new(bytes.Buffer),
	}
}

// WriteHeader catches the call to WriteHeader and doesn't actually do anything,
// except store the given code for later use when ActuallyWrite is called
func (brw *BufferedResponseWriter) WriteHeader(code int) {
	brw.Code = code
}

func (brw *BufferedResponseWriter) Write(b []byte) (int, error) {
	if brw.Code == 0 {
		brw.WriteHeader(200)
	}
	return brw.buffer.Write(b)
}

// Returns a buffer which will produce the original raw bytes from the
// response body. May be called multiple times, will return independent buffers
// each time. The buffers should NEVER be written to
func (brw *BufferedResponseWriter) originalBodyRaw() *bytes.Buffer {
	// We take the bytes in the existing buffer, and make a new buffer around
	// those bytes. This way we can read from the new buffer without draining
	// the original buffer
	return bytes.NewBuffer(brw.buffer.Bytes())
}

// GetBody returns a ReadCloser which gives the body of the response which was
// buffered. If the response was compressed the body will be transparently
// decompressed. Close must be called in order to clean up resources. GetBody
// may be called multiple times, each time it will return an independent
// ReadCloser for the original response body.
func (brw *BufferedResponseWriter) GetBody() (io.ReadCloser, error) {
	buf := brw.originalBodyRaw()
	switch brw.Header().Get("Content-Encoding") {
	case "gzip":
		return gzip.NewReader(buf)
	case "deflate":
		return flate.NewReader(buf), nil
	}
	return ioutil.NopCloser(buf), nil
}

// SetBody sets the io.Reader from which the new body of the buffered response
// should be read from. This io.Reader will be drained once ActuallyWrite is
// called. If this is never called the original body will be used.
//
// Note: there is no need to send in compressed data here, the new response body
// will be automatically compressed in the same way the original was.
func (brw *BufferedResponseWriter) SetBody(in io.Reader) {
	brw.newBody = in
}

// Returns a buffer containing the (potentially compressed) raw bytes which
// comprise the body which should be written. If SetBody has been called then
// that will be used as the body, otherwise the original bytes read in will be
func (brw *BufferedResponseWriter) bodyBuf() (*bytes.Buffer, error) {
	body := brw.originalBodyRaw()
	if brw.newBody == nil {
		return body, nil
	}

	// using body.Len() here is just a guess for how big we think the new body
	// will be
	bodyBuf := bytes.NewBuffer(make([]byte, 0, body.Len()))
	dst := brw.bodyEncoder(bodyBuf)
	defer dst.Close()

	// dst will be some wrapper around bodyBuf, or bodyBuf itself
	if _, err := io.Copy(dst, brw.newBody); err != nil {
		return nil, err
	}
	return bodyBuf, nil
}

// ActuallyWrite takes all the buffered data and actually writes to the wrapped
// ResponseWriter. Returns the number of bytes written as the body
func (brw *BufferedResponseWriter) ActuallyWrite() (int64, error) {
	bodyBuf, err := brw.bodyBuf()
	if err != nil {
		return 0, err
	}

	rw := brw.ResponseWriter
	rw.Header().Set("Content-Length", strconv.Itoa(bodyBuf.Len()))
	rw.WriteHeader(brw.Code)
	return io.Copy(rw, bodyBuf)
}

type nopWriteCloser struct {
	io.Writer
}

func (n nopWriteCloser) Close() error {
	if wc, ok := n.Writer.(io.WriteCloser); ok {
		return wc.Close()
	}
	return nil
}

func (brw *BufferedResponseWriter) bodyEncoder(w io.Writer) io.WriteCloser {
	switch brw.Header().Get("Content-Encoding") {
	case "gzip":
		return gzip.NewWriter(w)
	case "deflate":
		// From the docs: If level is in the range [-1, 9] then the error
		// returned will be nil. Otherwise the error returned will be non-nil
		// so we can ignore error
		dfW, _ := flate.NewWriter(w, -1)
		return dfW
	default:
		return nopWriteCloser{w}
	}
}

type brwMarshalled struct {
	Code   int
	Header map[string][]string
	Body   []byte
}

// MarshalBinary returns a binary form of the BufferedResponseWriter. Can only
// be called after a response has been buffered.
func (brw *BufferedResponseWriter) MarshalBinary() ([]byte, error) {
	body := brw.originalBodyRaw().Bytes()
	bm := brwMarshalled{
		Code:   brw.Code,
		Header: brw.Header(),
		Body:   body,
	}
	buf := new(bytes.Buffer)
	if err := gob.NewEncoder(buf).Encode(&bm); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UnmarshalBinary takes in a byte slice produced by calling MarshalBinary on
// another BufferedResponseWriter, and makes this one identical to it.
func (brw *BufferedResponseWriter) UnmarshalBinary(b []byte) error {
	var bm brwMarshalled
	if err := gob.NewDecoder(bytes.NewBuffer(b)).Decode(&bm); err != nil {
		return err
	}

	brw.Code = bm.Code

	// If there's any pre-set headers in the ResponseWriter, get rid of them
	h := brw.Header()
	for k := range h {
		delete(h, k)
	}

	// Now copy in the headers we want
	for k, vv := range bm.Header {
		for _, v := range vv {
			h.Add(k, v)
		}
	}

	// Copy in the body
	brw.buffer = bytes.NewBuffer(bm.Body)
	return nil
}
