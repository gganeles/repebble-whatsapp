package api

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const pingInterval = 25 * time.Second

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Access is controlled by the token; pkjs runs from an arbitrary origin.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	events, unsubscribe := s.hub.Subscribe()
	defer unsubscribe()

	// We never expect client frames; CloseRead handles pings/close and cancels ctx when the peer goes away.
	ctx := conn.CloseRead(r.Context())
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	write := func(v any) error {
		wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return wsjson.Write(wctx, conn, v)
	}
	if err := write(map[string]any{"type": "connection", "state": s.wa.Status().State}); err != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		case e, ok := <-events:
			if !ok {
				return
			}
			var msg map[string]any
			switch e.Type {
			case "message":
				msg = map[string]any{"type": "message", "chat": e.Chat, "message": s.messageJSON(*e.Message)}
			case "chat":
				msg = map[string]any{"type": "chat", "chat": s.chatJSON(*e.ChatRow)}
			case "receipt":
				msg = map[string]any{"type": "receipt", "chat": e.Chat, "ids": e.IDs, "status": e.Status}
			case "connection":
				msg = map[string]any{"type": "connection", "state": e.State}
			default:
				continue
			}
			if err := write(msg); err != nil {
				return
			}
		}
	}
}
