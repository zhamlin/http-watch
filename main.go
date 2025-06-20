package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/zhamlin/http-watch/internal"
)

type config struct {
	addr      string
	logLevel  string
	dir       string
	pattern   string
	recursive bool
	tlsCert   string
	tlsKey    string
	gzip      bool
}

func (c config) hasTLS() bool {
	return c.tlsKey != "" && c.tlsCert != ""
}

func loadConfig() config {
	cfg := config{}
	flag.StringVar(&cfg.addr, "addr", "localhost:8080", "address to listen on")
	flag.StringVar(&cfg.dir, "dir", "", "directory to serve via /")
	flag.StringVar(&cfg.pattern, "pattern", "", "file matching pattern")
	flag.StringVar(&cfg.logLevel, "log.level", "info", "slog log level to use")
	flag.BoolVar(&cfg.recursive, "recursive", true, "watch all files recursively")
	flag.StringVar(&cfg.tlsCert, "cert", "", "tls cert")
	flag.StringVar(&cfg.tlsKey, "key", "", "tls key")
	flag.BoolVar(&cfg.gzip, "gzip", true, "Use gzip compression")

	flag.Parse()
	return cfg
}

func strToLogLevel(str string) slog.Level {
	switch strings.ToUpper(str) {
	case slog.LevelError.String():
		return slog.LevelError
	case slog.LevelWarn.String():
		return slog.LevelWarn
	case slog.LevelInfo.String():
		return slog.LevelInfo
	case slog.LevelDebug.String():
		return slog.LevelDebug
	}
	panic("invalid slog log level: " + str)
}

func setupSlog(level slog.Level) {
	var logLevel slog.LevelVar
	logLevel.Set(level)

	handler := internal.NewHandler(os.Stderr, &internal.ColorOptions{
		Level:      &logLevel,
		TimeFormat: time.DateTime,
	})

	logger := slog.New(handler)
	slog.SetDefault(logger)
}

func main() {
	cfg := loadConfig()
	setupSlog(strToLogLevel(cfg.logLevel))
	ctx := context.Background()

	if err := run(ctx, cfg); err != nil {
		slog.Error("run failed", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config) error {
	ctx, signalCancel := signal.NotifyContext(ctx, os.Interrupt)
	defer signalCancel()

	b := NewBroadcaster()

	if cfg.pattern != "" {
		fn, err := createWatcherFn(cfg, b)
		if err != nil {
			return fmt.Errorf("createWatcherFn: %w", err)
		}
		go fn()
	}

	h := http.NewServeMux()
	h.Handle("GET /changed/{file...}", newFileChangedHandler(b))
	h.Handle("GET /ws", newWebsocketHandler(b))

	if cfg.dir != "" {
		f := os.DirFS(cfg.dir)
		fileHandler := func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
			w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
			w.Header().Set("Cache-Control", "max-age=0")
			w.Header().Set("Access-Control-Allow-Origin", "*")

			http.FileServerFS(f).ServeHTTP(w, r)
		}

		var compressionChoices []string
		if cfg.gzip {
			compressionChoices = append(compressionChoices, "gzip")
		}
		h.Handle("GET /", makeCompressionHandler(fileHandler, compressionChoices))
	}

	s := http.Server{
		Addr:    cfg.addr,
		Handler: h,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
		ReadTimeout:  time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  20 * time.Second,
	}

	srvErr := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", cfg.addr)

		listenAndServe := s.ListenAndServe
		if cfg.hasTLS() {
			listenAndServe = func() error {
				return s.ListenAndServeTLS(cfg.tlsCert, cfg.tlsKey)
			}
		}

		if err := listenAndServe(); err != http.ErrServerClosed {
			srvErr <- err
		}
	}()

	select {
	case err := <-srvErr:
		return err
	case <-ctx.Done():
		// Wait for CTRL+C.
	}

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(
		context.Background(), 5*time.Second,
	)
	defer cancel()
	return s.Shutdown(shutdownCtx)
}
