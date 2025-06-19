package main

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

type compressedResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w compressedResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func makeGzipHandler(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		gzr := compressedResponseWriter{Writer: gz, ResponseWriter: w}
		fn(gzr, r)
	}
}

var comressionFnByName = map[string]func(http.HandlerFunc) http.HandlerFunc{
	"gzip": makeGzipHandler,
}

func makeCompressionHandler(fn http.HandlerFunc, compresionPrefs []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		encodingHeader := r.Header.Get("Accept-Encoding")
		for _, compression := range compresionPrefs {
			wantsCompressionType := strings.Contains(encodingHeader, compression)
			if wantsCompressionType {
				compressFn, has := comressionFnByName[compression]
				if has {
					compressFn(fn)(w, r)
					return
				}
			}
		}

		fn(w, r)
	}
}
