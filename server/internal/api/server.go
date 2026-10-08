// Package api serves the localhost HTTP + WebSocket API that PebbleKit JS talks to.
// See docs/protocol.md §1.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/repebble/repebble-whatsapp/server/internal/hub"
	"github.com/repebble/repebble-whatsapp/server/internal/store"
	"github.com/repebble/repebble-whatsapp/server/internal/wa"
	"github.com/repebble/repebble-whatsapp/server/internal/watchtext"
)

// WhatsApp is the part of wa.Client the API needs.
type WhatsApp interface {
	Status() wa.Status
	Pair(ctx context.Context, phone string) (string, error)
	Send(ctx context.Context, chatJID, text string) (store.Message, error)
	MarkRead(ctx context.Context, chatJID, upToID string) error
	Logout(ctx context.Context) error
	// FetchImage returns the original image bytes for a message with a picture.
	FetchImage(ctx context.Context, chatJID, id string) ([]byte, error)
}

type Server struct {
	wa    WhatsApp
	store *store.Store
	hub   *hub.Hub
	token string
	text  watchtext.Options
	mux   *http.ServeMux
}

func New(w WhatsApp, st *store.Store, h *hub.Hub, token string, text watchtext.Options) *Server {
	s := &Server{wa: w, store: st, hub: h, token: token, text: text, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /v1/status", s.handleStatus)
	s.mux.HandleFunc("POST /v1/pair", s.handlePair)
	s.mux.HandleFunc("POST /v1/logout", s.handleLogout)
	s.mux.HandleFunc("GET /v1/chats", s.handleChats)
	s.mux.HandleFunc("GET /v1/chats/{jid}/messages", s.handleMessages)
	s.mux.HandleFunc("POST /v1/chats/{jid}/messages", s.handleSend)
	s.mux.HandleFunc("GET /v1/chats/{jid}/messages/{id}", s.handleMessage)
	s.mux.HandleFunc("GET /v1/chats/{jid}/messages/{id}/image", s.handleImage)
	s.mux.HandleFunc("POST /v1/chats/{jid}/read", s.handleRead)
	s.mux.HandleFunc("GET /v1/replies", s.handleGetReplies)
	s.mux.HandleFunc("PUT /v1/replies", s.handlePutReplies)
	s.mux.HandleFunc("GET /v1/events", s.handleEvents)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Settings pages and pkjs may send preflights; allow them without auth.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "bad_token", "missing or wrong token")
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) authorized(r *http.Request) bool {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" {
		got = r.URL.Query().Get("token")
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]apiError{"error": {Code: code, Message: msg}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 64<<10)).Decode(v)
}

// waError maps wa errors onto API error codes.
func waError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, wa.ErrNotPaired):
		writeError(w, http.StatusConflict, "not_paired", err.Error())
	case errors.Is(err, wa.ErrDisconnected):
		writeError(w, http.StatusServiceUnavailable, "wa_disconnected", err.Error())
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	default:
		writeError(w, http.StatusBadGateway, fallback, err.Error())
	}
}
