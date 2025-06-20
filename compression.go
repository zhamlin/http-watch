package httpwatch

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

type compressedResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w compressedResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

var gzipPool = sync.Pool{
	New: func() any {
		return gzip.NewWriter(nil)
	},
}

func newGzipHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")

		gz := gzipPool.Get().(*gzip.Writer)
		gz.Reset(w)
		defer func() {
			_ = gz.Close()
			gzipPool.Put(gz)
		}()

		gzr := compressedResponseWriter{Writer: gz, ResponseWriter: w}
		next.ServeHTTP(gzr, r)
	})
}
