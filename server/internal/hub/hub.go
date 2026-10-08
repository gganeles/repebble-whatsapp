// Package hub fans live WhatsApp events out to connected WebSocket clients.
package hub

import (
	"sync"

	"github.com/repebble/repebble-whatsapp/server/internal/store"
)

type Event struct {
	Type    string         // "message", "chat", "receipt", "connection"
	Chat    string         // chat JID, for message/receipt
	Message *store.Message // for "message"
	ChatRow *store.Chat    // for "chat"
	IDs     []string       // for "receipt"
	Status  string         // receipt status
	State   string         // for "connection"
}

type Hub struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

func New() *Hub { return &Hub{subs: map[chan Event]struct{}{}} }

// Subscribe returns a buffered channel of events and a function to unsubscribe.
func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// Publish delivers e to every subscriber, dropping it for subscribers that are too far behind.
func (h *Hub) Publish(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
		}
	}
}
