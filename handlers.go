package main

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

func newWebsocketHandler(b *Broadcaster) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			slog.Error("websocket.Accept", "err", err)
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")

		slog.Info("websocket connected")
		subscriber := b.AddSubscriber()
		defer b.Remove(subscriber)

		pingTicker := time.NewTicker(30 * time.Second)
		defer pingTicker.Stop()

		ctx := r.Context()
		go func() {
			for {
				_, _, err := c.Reader(ctx)
				if err != nil {
					slog.Debug("websocket client disconnected or read error", "err", err)
					c.Close(websocket.StatusNormalClosure, "client read failure")
					return
				}
			}
		}()

		for {
			select {
			case <-ctx.Done():
				slog.Debug("websocket context done")
				return
			case <-pingTicker.C:
				err := c.Ping(ctx)
				if err != nil {
					slog.Debug("websocket ping error", "err", err)
					return
				}
			case filename := <-subscriber:
				slog.Debug("websocket got file change", "file", filename)
				err := c.Write(ctx, websocket.MessageText, []byte(filename))
				if err != nil {
					slog.Debug("websocket.Write", "err", err)
					return
				}
			}
		}
	}
}

func newFileChangedHandler(b *Broadcaster) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filename := r.PathValue("file")
		b.Broadcast(filename)
	}
}
