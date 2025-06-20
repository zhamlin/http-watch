package httpwatch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/coder/websocket"
)

type websocketMessage struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

func writeMessage(ctx context.Context, c *websocket.Conn, msg websocketMessage) error {
	data, err := json.Marshal(&msg)
	if err != nil {
		return fmt.Errorf("json.Marshal: %w", err)
	}

	err = c.Write(ctx, websocket.MessageText, data)
	if err != nil {
		return fmt.Errorf("websocket.Write: %w", err)
	}

	return nil
}

func NewWebsocketHandler(b *Broadcaster) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			slog.ErrorContext(ctx, "websocket.Accept", "err", err)
			return
		}

		wsCtx := c.CloseRead(context.Background())
		closeFn := func(code websocket.StatusCode) {
			err := c.Close(code, "")
			slog.DebugContext(ctx,
				"websocket closed",
				"code", code,
				"err", err,
			)
		}
		defer closeFn(websocket.StatusNormalClosure)

		slog.InfoContext(ctx, "websocket connected")
		subscriber := b.AddSubscriber()
		defer b.Remove(subscriber)

		pingTicker := time.NewTicker(30 * time.Second)
		defer pingTicker.Stop()

		for {
			select {
			case <-wsCtx.Done():
				slog.DebugContext(ctx, "websocket CloseRead context done")
				return
			case <-ctx.Done():
				slog.DebugContext(ctx, "websocket context done")
				return
			case <-pingTicker.C:
				err := c.Ping(ctx)
				if err != nil {
					slog.DebugContext(ctx, "websocket ping error", "err", err)
					return
				}
			case filename := <-subscriber:
				slog.DebugContext(ctx, "websocket got file change", "file", filename)
				msg := websocketMessage{
					Type: "file.change",
					Data: filename,
				}

				if err := writeMessage(ctx, c, msg); err != nil {
					slog.DebugContext(ctx, "writeMessage", "err", err, "msg", msg)
					return
				}
			}
		}
	}
}

func NewFileServer(dir string, useGzip bool) http.Handler {
	f := os.DirFS(dir)
	handler := http.FileServerFS(f)

	if useGzip {
		return newGzipHandler(handler)
	}
	return handler
}

// HeaderMiddleware returns a middleware that sets one or more values per header key
func HeaderMiddleware(headers http.Header) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for key, values := range headers {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
