package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	httpwatch "github.com/zhamlin/http-watch"
	"github.com/zhamlin/http-watch/internal"
)

type config struct {
	httpwatch.WatcherConfig
	addr     string
	logLevel string
	tlsCert  string
	tlsKey   string
	gzip     bool
}

func (c config) hasTLS() bool {
	return c.tlsKey != "" && c.tlsCert != ""
}

func loadConfig() config {
	cfg := config{}
	flag.StringVar(&cfg.addr, "addr", "localhost:8080", "address to listen on")
	flag.StringVar(&cfg.Dir, "dir", "", "directory to serve via /")
	flag.StringVar(&cfg.FilePattern, "pattern", "", "file matching pattern")
	flag.StringVar(&cfg.logLevel, "log.level", "info", "slog log level to use")
	flag.StringVar(&cfg.tlsCert, "tls.cert", "", "tls cert")
	flag.StringVar(&cfg.tlsKey, "tls.key", "", "tls key")
	flag.BoolVar(&cfg.Recursive, "recursive", true, "watch all files recursively")
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
		slog.ErrorContext(ctx, "run failed", slog.Any("error", err))
		os.Exit(1)
	}
}

func runServer(ctx context.Context, s *http.Server, cfg config) error {
	srvErr := make(chan error, 1)
	go func() {
		slog.InfoContext(ctx, "listening for requests", "addr", s.Addr)

		listenAndServe := s.ListenAndServe
		if cfg.hasTLS() {
			listenAndServe = func() error {
				return s.ListenAndServeTLS(cfg.tlsCert, cfg.tlsKey)
			}
		}

		if err := listenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			srvErr <- err
		}
	}()

	// Wait for interruption.
	select {
	case err := <-srvErr:
		return fmt.Errorf("server.ListenAndServe(): %w", err)
	case <-ctx.Done():
		// Wait for first CTRL+C.
		// Stop receiving signal notifications as soon as possible.
	}

	slog.InfoContext(ctx, "shutting down")
	shutdownCtx, cancel := context.WithTimeout(
		context.Background(), 5*time.Second,
	)
	defer cancel()
	return s.Shutdown(shutdownCtx)
}

func run(ctx context.Context, cfg config) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	b := httpwatch.NewBroadcaster()
	h := http.NewServeMux()

	if cfg.FilePattern != "" {
		fn, err := httpwatch.NewWatcherFn(ctx, cfg.WatcherConfig, b)
		if err != nil {
			return fmt.Errorf("createWatcherFn: %w", err)
		}
		go fn()
		h.Handle("GET /_/events", httpwatch.NewWebsocketHandler(b))
	}

	if cfg.Dir != "" {
		headers := http.Header{}
		headers.Set("Cross-Origin-Opener-Policy", "same-origin")
		headers.Set("Cross-Origin-Embedder-Policy", "require-corp")
		headers.Set("Cache-Control", "max-age=0")
		headers.Set("Access-Control-Allow-Origin", "*")

		headersMW := httpwatch.HeaderMiddleware(headers)
		handler := httpwatch.NewFileServer(cfg.Dir, cfg.gzip)
		h.Handle("GET /", headersMW(handler))
	}

	s := &http.Server{
		Addr:    cfg.addr,
		Handler: h,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
		ReadTimeout:  time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  20 * time.Second,
	}
	return runServer(ctx, s, cfg)
}
