package gin_gonic

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
)

type TestResponseWriter struct {
	ResponseWriter *httptest.ResponseRecorder
	WrittenRC      bool
}

func NewTestResponseWriter() *TestResponseWriter {
	return &TestResponseWriter{
		ResponseWriter: httptest.NewRecorder(),
	}
}

func (t *TestResponseWriter) Header() http.Header {
	return t.ResponseWriter.Header()
}

func (t *TestResponseWriter) Write(buf []byte) (int, error) {
	t.WrittenRC = true
	return t.ResponseWriter.Write(buf)
}

func (t *TestResponseWriter) WriteHeader(sc int) {
	t.ResponseWriter.WriteHeader(sc)
}

func (t *TestResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, nil
}

func (t *TestResponseWriter) Flush() {

}

func (t *TestResponseWriter) CloseNotify() <-chan bool {
	return make(<-chan bool, 1)
}

func (t *TestResponseWriter) Status() int {
	return t.ResponseWriter.Code
}

func (t *TestResponseWriter) Size() int {
	return len(t.ResponseWriter.Body.Bytes())
}

func (t *TestResponseWriter) WriteString(s string) (int, error) {
	return t.WriteString(s)
}

func (t *TestResponseWriter) Written() bool {
	return t.WrittenRC
}

func (t *TestResponseWriter) WriteHeaderNow() {
}

func (t *TestResponseWriter) Pusher() http.Pusher {
	return nil
}
