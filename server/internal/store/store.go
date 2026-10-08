// Package store keeps pebblewa's own copy of chats and messages. whatsmeow only
// persists session keys, so everything the watch browses comes from here.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

// DefaultReplies seeds the canned reply list on first run.
var DefaultReplies = []string{
	"OK",
	"On my way",
	"Can't talk now, will reply later",
	"Yes",
	"No",
	"Thanks!",
	"Call you soon",
	"(y)",
}

type Store struct {
	db *sql.DB
}

type Chat struct {
	JID      string `json:"jid"`
	Name     string `json:"name"`
	Group    bool   `json:"group"`
	Archived bool   `json:"archived"`
	Muted    bool   `json:"muted"`
	Unread   int    `json:"unread"`
	LastTS   int64  `json:"ts"`

	// Filled from the latest message.
	LastText   string `json:"-"`
	LastFromMe bool   `json:"from_me"`
	LastSender string `json:"-"`
}

type Message struct {
	ChatJID    string `json:"-"`
	ID         string `json:"id"`
	SenderJID  string `json:"-"`
	SenderName string `json:"sender,omitempty"`
	FromMe     bool   `json:"from_me"`
	TS         int64  `json:"ts"`
	Kind       string `json:"kind"`
	Text       string `json:"text"`
	Status     string `json:"status"`
	HasImage   bool   `json:"-"`
}

// Media is what's needed to fetch a message's picture.
type Media struct {
	Kind      string
	Proto     []byte
	Thumbnail []byte
}

var ErrNotFound = errors.New("not found")

func Open(ctx context.Context, path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	s := &Store{db: db}
	if err := s.seedReplies(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) seedReplies(ctx context.Context) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM replies`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	return s.SetReplies(ctx, DefaultReplies)
}

// UpsertChat creates the chat if needed. Empty name or false isGroup never overwrite known values.
func (s *Store) UpsertChat(ctx context.Context, jid, name string, isGroup bool) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO chats (jid, name, is_group) VALUES (?, ?, ?)
		ON CONFLICT (jid) DO UPDATE SET
			name = CASE WHEN excluded.name != '' THEN excluded.name ELSE chats.name END,
			is_group = MAX(chats.is_group, excluded.is_group)`,
		jid, name, isGroup)
	return err
}

func (s *Store) SetChatName(ctx context.Context, jid, name string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET name = ? WHERE jid = ?`, name, jid)
	return err
}

func (s *Store) ChatName(ctx context.Context, jid string) (string, error) {
	var name string
	err := s.db.QueryRowContext(ctx, `SELECT name FROM chats WHERE jid = ?`, jid).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return name, err
}

func (s *Store) SetArchived(ctx context.Context, jid string, archived bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET archived = ? WHERE jid = ?`, archived, jid)
	return err
}

func (s *Store) SetMuted(ctx context.Context, jid string, muted bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET muted = ? WHERE jid = ?`, muted, jid)
	return err
}

// SetUnread overrides the unread counter, e.g. from a history sync or a read on another device.
func (s *Store) SetUnread(ctx context.Context, jid string, unread int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET unread = ? WHERE jid = ?`, unread, jid)
	if err == nil && unread == 0 {
		_, err = s.db.ExecContext(ctx, `UPDATE messages SET seen = 1 WHERE chat_jid = ? AND from_me = 0`, jid)
	}
	return err
}

// AddMessage stores a message and bumps the chat. countUnread increments the unread
// counter for new incoming messages (live ones; history sync sets it directly).
// Returns false if the message was already stored.
func (s *Store) AddMessage(ctx context.Context, m Message, countUnread bool) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO messages (chat_jid, id, sender_jid, sender_name, from_me, ts, kind, text, status, seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (chat_jid, id) DO NOTHING`,
		m.ChatJID, m.ID, m.SenderJID, m.SenderName, m.FromMe, m.TS, m.Kind, m.Text, m.Status, m.FromMe || !countUnread)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, tx.Commit()
	}
	inc := 0
	if countUnread && !m.FromMe {
		inc = 1
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO chats (jid, last_ts, unread) VALUES (?, ?, ?)
		ON CONFLICT (jid) DO UPDATE SET
			last_ts = MAX(chats.last_ts, excluded.last_ts),
			unread = CASE WHEN ? THEN 0 ELSE chats.unread + excluded.unread END`,
		m.ChatJID, m.TS, inc, m.FromMe && countUnread); err != nil {
		return false, err
	}
	if m.FromMe && countUnread {
		// Replying from any device means everything before it was read.
		if _, err := tx.ExecContext(ctx, `UPDATE messages SET seen = 1 WHERE chat_jid = ? AND from_me = 0 AND ts <= ?`,
			m.ChatJID, m.TS); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

func (s *Store) MessageTS(ctx context.Context, chat, id string) (int64, error) {
	var ts int64
	err := s.db.QueryRowContext(ctx, `SELECT ts FROM messages WHERE chat_jid = ? AND id = ?`, chat, id).Scan(&ts)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return ts, err
}

// EditMessage replaces the text of a stored message (edits and deletions).
func (s *Store) EditMessage(ctx context.Context, chat, id, text string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE messages SET text = ? WHERE chat_jid = ? AND id = ?`, text, chat, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

var statusRank = map[string]int{"pending": 0, "sent": 1, "delivered": 2, "read": 3, "played": 3}

// SetStatus upgrades the delivery status of our own messages; it never downgrades.
func (s *Store) SetStatus(ctx context.Context, chat string, ids []string, status string) error {
	rank := statusRank[status]
	for _, id := range ids {
		var cur string
		err := s.db.QueryRowContext(ctx, `SELECT status FROM messages WHERE chat_jid = ? AND id = ? AND from_me = 1`, chat, id).Scan(&cur)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		} else if err != nil {
			return err
		}
		if statusRank[cur] < rank {
			if _, err := s.db.ExecContext(ctx, `UPDATE messages SET status = ? WHERE chat_jid = ? AND id = ?`, status, chat, id); err != nil {
				return err
			}
		}
	}
	return nil
}

const chatColumns = `
	c.jid, c.name, c.is_group, c.archived, c.muted, c.unread, c.last_ts,
	COALESCE(m.text, ''), COALESCE(m.from_me, 0), COALESCE(m.sender_name, '')`

const chatJoin = `
	FROM chats c
	LEFT JOIN messages m ON m.chat_jid = c.jid AND m.id = (
		SELECT id FROM messages WHERE chat_jid = c.jid ORDER BY ts DESC, rowid DESC LIMIT 1)`

func scanChat(sc interface{ Scan(...any) error }) (Chat, error) {
	var c Chat
	err := sc.Scan(&c.JID, &c.Name, &c.Group, &c.Archived, &c.Muted, &c.Unread, &c.LastTS,
		&c.LastText, &c.LastFromMe, &c.LastSender)
	return c, err
}

// ListChats returns chats newest first. beforeTS/beforeJID is the keyset cursor (zero for the first page).
func (s *Store) ListChats(ctx context.Context, limit int, beforeTS int64, beforeJID string, archived bool) ([]Chat, error) {
	q := `SELECT ` + chatColumns + chatJoin + ` WHERE c.last_ts > 0`
	var args []any
	if !archived {
		q += ` AND c.archived = 0`
	}
	if beforeTS > 0 {
		q += ` AND (c.last_ts < ? OR (c.last_ts = ? AND c.jid < ?))`
		args = append(args, beforeTS, beforeTS, beforeJID)
	}
	q += ` ORDER BY c.last_ts DESC, c.jid DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chat
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetChat(ctx context.Context, jid string) (Chat, error) {
	c, err := scanChat(s.db.QueryRowContext(ctx, `SELECT `+chatColumns+chatJoin+` WHERE c.jid = ?`, jid))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

const messageColumns = `m.chat_jid, m.id, m.sender_jid, m.sender_name, m.from_me, m.ts, m.kind, m.text, m.status,
	EXISTS (SELECT 1 FROM media WHERE media.chat_jid = m.chat_jid AND media.id = m.id)`

func scanMessage(sc interface{ Scan(...any) error }) (Message, error) {
	var m Message
	err := sc.Scan(&m.ChatJID, &m.ID, &m.SenderJID, &m.SenderName, &m.FromMe, &m.TS, &m.Kind, &m.Text, &m.Status, &m.HasImage)
	return m, err
}

// GetMessage returns one message with its full text.
func (s *Store) GetMessage(ctx context.Context, chat, id string) (Message, error) {
	m, err := scanMessage(s.db.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages m WHERE chat_jid = ? AND id = ?`, chat, id))
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// SaveMedia records how to download a message's picture.
func (s *Store) SaveMedia(ctx context.Context, chat, id string, media Media) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO media (chat_jid, id, kind, proto, thumbnail) VALUES (?, ?, ?, ?, ?)`,
		chat, id, media.Kind, media.Proto, media.Thumbnail)
	return err
}

func (s *Store) GetMedia(ctx context.Context, chat, id string) (Media, error) {
	var m Media
	err := s.db.QueryRowContext(ctx, `SELECT kind, proto, thumbnail FROM media WHERE chat_jid = ? AND id = ?`, chat, id).
		Scan(&m.Kind, &m.Proto, &m.Thumbnail)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// ListMessages returns up to limit messages older than beforeID (or the newest ones), oldest first.
func (s *Store) ListMessages(ctx context.Context, chat string, limit int, beforeID string) ([]Message, bool, error) {
	q := `SELECT ` + messageColumns + ` FROM messages m WHERE chat_jid = ?`
	args := []any{chat}
	if beforeID != "" {
		var ts int64
		var rowid int64
		err := s.db.QueryRowContext(ctx, `SELECT ts, rowid FROM messages WHERE chat_jid = ? AND id = ?`, chat, beforeID).Scan(&ts, &rowid)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, ErrNotFound
		} else if err != nil {
			return nil, false, err
		}
		q += ` AND (ts < ? OR (ts = ? AND rowid < ?))`
		args = append(args, ts, ts, rowid)
	}
	q += ` ORDER BY ts DESC, rowid DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, more, nil
}

// UnseenIncoming returns unread incoming messages up to and including upToID (all if empty),
// grouped by sender JID, for sending read receipts.
func (s *Store) UnseenIncoming(ctx context.Context, chat, upToID string) (map[string][]string, int64, error) {
	q := `SELECT id, sender_jid, ts FROM messages WHERE chat_jid = ? AND from_me = 0 AND seen = 0`
	args := []any{chat}
	if upToID != "" {
		q += ` AND ts <= (SELECT ts FROM messages WHERE chat_jid = ? AND id = ?)`
		args = append(args, chat, upToID)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := map[string][]string{}
	var maxTS int64
	for rows.Next() {
		var id, sender string
		var ts int64
		if err := rows.Scan(&id, &sender, &ts); err != nil {
			return nil, 0, err
		}
		out[sender] = append(out[sender], id)
		maxTS = max(maxTS, ts)
	}
	return out, maxTS, rows.Err()
}

// MarkSeen flags incoming messages up to ts as read and recomputes the unread counter.
func (s *Store) MarkSeen(ctx context.Context, chat string, upToTS int64) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE messages SET seen = 1 WHERE chat_jid = ? AND from_me = 0 AND ts <= ?`, chat, upToTS); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE chats SET unread = (SELECT COUNT(*) FROM messages WHERE chat_jid = ? AND from_me = 0 AND seen = 0)
		WHERE jid = ?`, chat, chat)
	return err
}

// LookupClientID returns the message previously sent for a client_id, for idempotent retries.
func (s *Store) LookupClientID(ctx context.Context, clientID string) (msgID string, ts int64, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT message_id, ts FROM sent_client_ids WHERE client_id = ?`, clientID).Scan(&msgID, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, false, nil
	}
	return msgID, ts, err == nil, err
}

func (s *Store) RememberClientID(ctx context.Context, clientID, chat, msgID string, ts int64) error {
	if _, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO sent_client_ids VALUES (?, ?, ?, ?)`, clientID, chat, msgID, ts); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM sent_client_ids WHERE client_id NOT IN (
		SELECT client_id FROM sent_client_ids ORDER BY ts DESC LIMIT 100)`)
	return err
}

func (s *Store) Replies(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT text FROM replies ORDER BY pos`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) SetReplies(ctx context.Context, replies []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM replies`); err != nil {
		return err
	}
	pos := 0
	for _, r := range replies {
		if r = strings.TrimSpace(r); r == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO replies (pos, text) VALUES (?, ?)`, pos, r); err != nil {
			return err
		}
		pos++
	}
	return tx.Commit()
}
