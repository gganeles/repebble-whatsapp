package api

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/repebble/repebble-whatsapp/server/internal/store"
	"github.com/repebble/repebble-whatsapp/server/internal/wa"
	"github.com/repebble/repebble-whatsapp/server/internal/watchimg"
	"github.com/repebble/repebble-whatsapp/server/internal/watchtext"
)

const (
	maxChatPage    = 20
	maxMessagePage = 30
	maxReplies     = 8
	maxSendBytes   = 4096
)

type chatJSON struct {
	JID     string `json:"jid"`
	Name    string `json:"name"`
	Preview string `json:"preview"`
	TS      int64  `json:"ts"`
	Unread  int    `json:"unread"`
	Group   bool   `json:"group"`
	Muted   bool   `json:"muted"`
	FromMe  bool   `json:"from_me"`
}

type messageJSON struct {
	ID     string `json:"id"`
	FromMe bool   `json:"from_me"`
	Sender string `json:"sender,omitempty"`
	Text   string `json:"text"`
	TS     int64  `json:"ts"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	// Truncated means text was cut; GET .../messages/{id} has the full text.
	Truncated bool `json:"truncated,omitempty"`
	HasImage  bool `json:"has_image,omitempty"`
}

func (s *Server) chatJSON(c store.Chat) chatJSON {
	name := c.Name
	if name == "" {
		name = strings.SplitN(c.JID, "@", 2)[0]
		if !c.Group {
			name = "+" + name
		}
	}
	preview := c.LastText
	switch {
	case c.LastFromMe:
		preview = "You: " + preview
	case c.Group && c.LastSender != "":
		preview = firstWord(c.LastSender) + ": " + preview
	}
	unread := min(c.Unread, 99)
	return chatJSON{
		JID: c.JID, Name: watchtext.Fit(name, watchtext.ChatName, s.text),
		Preview: watchtext.Fit(preview, watchtext.Preview, s.text),
		TS:      c.LastTS, Unread: unread, Group: c.Group, Muted: c.Muted, FromMe: c.LastFromMe,
	}
}

func (s *Server) messageJSON(m store.Message) messageJSON {
	return s.messageJSONLimit(m, watchtext.MessageText)
}

func (s *Server) messageJSONLimit(m store.Message, limit int) messageJSON {
	clean := watchtext.Clean(m.Text, s.text)
	out := messageJSON{
		ID: m.ID, FromMe: m.FromMe, TS: m.TS, Kind: m.Kind, Status: m.Status,
		Text: watchtext.Truncate(clean, limit), Truncated: len(clean) > limit, HasImage: m.HasImage,
	}
	if m.SenderName != "" {
		out.Sender = watchtext.Fit(m.SenderName, watchtext.SenderName, s.text)
	}
	if out.Text == "" {
		out.Text = "[Message]"
	}
	return out
}

func firstWord(name string) string {
	if strings.HasPrefix(name, "+") {
		return name
	}
	if i := strings.IndexByte(name, ' '); i > 0 {
		return name[:i]
	}
	return name
}

func intParam(r *http.Request, key string, def, max int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil || v <= 0 {
		return def
	}
	return min(v, max)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.wa.Status())
}

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
	}
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Phone) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "phone is required, e.g. +491512345678")
		return
	}
	code, err := s.wa.Pair(r.Context(), req.Phone)
	if err != nil {
		if err == wa.ErrAlreadyPaired {
			writeError(w, http.StatusConflict, "already_paired", err.Error())
			return
		}
		waError(w, err, "pair_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"pairing_code": code})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.wa.Logout(r.Context()); err != nil {
		waError(w, err, "logout_failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Chat cursors are "<last_ts>_<jid>" of the last row on the previous page.
func (s *Server) handleChats(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 10, maxChatPage)
	var beforeTS int64
	var beforeJID string
	if cur := r.URL.Query().Get("before"); cur != "" {
		ts, jid, ok := strings.Cut(cur, "_")
		var err error
		if beforeTS, err = strconv.ParseInt(ts, 10, 64); !ok || err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "bad cursor")
			return
		}
		beforeJID = jid
	}
	archived := r.URL.Query().Get("archived") == "1"
	chats, err := s.store.ListChats(r.Context(), limit+1, beforeTS, beforeJID, archived)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	var next *string
	if len(chats) > limit {
		chats = chats[:limit]
		last := chats[len(chats)-1]
		cur := fmt.Sprintf("%d_%s", last.LastTS, last.JID)
		next = &cur
	}
	out := make([]chatJSON, 0, len(chats))
	for _, c := range chats {
		out = append(out, s.chatJSON(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"chats": out, "next": next})
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	jid := r.PathValue("jid")
	limit := intParam(r, "limit", 15, maxMessagePage)
	msgs, more, err := s.store.ListMessages(r.Context(), jid, limit, r.URL.Query().Get("before"))
	if err != nil {
		waError(w, err, "internal")
		return
	}
	out := make([]messageJSON, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, s.messageJSON(m))
	}
	var next *string
	if more && len(msgs) > 0 {
		next = &msgs[0].ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out, "next": next})
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	jid := r.PathValue("jid")
	var req struct {
		Text     string `json:"text"`
		ClientID string `json:"client_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" || len(req.Text) > maxSendBytes || !utf8.ValidString(req.Text) {
		writeError(w, http.StatusBadRequest, "bad_request", "text must be 1-4096 bytes of UTF-8")
		return
	}
	ctx := r.Context()
	if req.ClientID != "" {
		if id, ts, ok, err := s.store.LookupClientID(ctx, req.ClientID); err == nil && ok {
			writeJSON(w, http.StatusOK, map[string]any{"id": id, "ts": ts, "client_id": req.ClientID})
			return
		}
	}
	m, err := s.wa.Send(ctx, jid, req.Text)
	if err != nil {
		waError(w, err, "send_failed")
		return
	}
	if req.ClientID != "" {
		_ = s.store.RememberClientID(ctx, req.ClientID, jid, m.ID, m.TS)
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": m.ID, "ts": m.TS, "client_id": req.ClientID})
}

func (s *Server) handleRead(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UpTo string `json:"up_to"`
	}
	_ = readJSON(r, &req) // body is optional
	if err := s.wa.MarkRead(r.Context(), r.PathValue("jid"), req.UpTo); err != nil {
		waError(w, err, "read_failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetReplies(w http.ResponseWriter, r *http.Request) {
	replies, err := s.store.Replies(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := make([]string, 0, len(replies))
	for _, t := range replies {
		out = append(out, watchtext.Fit(t, watchtext.ReplyText, s.text))
	}
	writeJSON(w, http.StatusOK, map[string]any{"replies": out})
}

func (s *Server) handlePutReplies(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Replies []string `json:"replies"`
	}
	if err := readJSON(r, &req); err != nil || len(req.Replies) > maxReplies {
		writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("expected {\"replies\": [...]} with at most %d entries", maxReplies))
		return
	}
	if err := s.store.SetReplies(r.Context(), req.Replies); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	s.handleGetReplies(w, r)
}

func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.GetMessage(r.Context(), r.PathValue("jid"), r.PathValue("id"))
	if err != nil {
		waError(w, err, "internal")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": s.messageJSONLimit(m, watchtext.FullText)})
}

// handleImage returns a message's picture as a raw Pebble bitmap, base64 encoded
// because PebbleKit JS can't be relied on to handle binary responses.
func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	maxW := intParam(r, "w", 144, watchimg.MaxWidth)
	maxH := intParam(r, "h", 144, watchimg.MaxHeight)
	format := watchimg.Color8
	if r.URL.Query().Get("format") == string(watchimg.BW1) {
		format = watchimg.BW1
	}
	src, err := s.wa.FetchImage(r.Context(), r.PathValue("jid"), r.PathValue("id"))
	if err != nil {
		waError(w, err, "download_failed")
		return
	}
	bm, err := watchimg.Convert(src, maxW, maxH, format)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "bad_image", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"width": bm.Width, "height": bm.Height, "format": bm.Format, "row_bytes": bm.RowBytes,
		"data": base64.StdEncoding.EncodeToString(bm.Data),
	})
}
