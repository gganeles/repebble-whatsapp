package store

import (
	"context"
	"path/filepath"
	"testing"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestChatsAndMessages(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.UpsertChat(ctx, "a@s.whatsapp.net", "Alice", false))
	must(s.UpsertChat(ctx, "g@g.us", "Group", true))
	for i, m := range []Message{
		{ChatJID: "a@s.whatsapp.net", ID: "1", SenderJID: "a@s.whatsapp.net", TS: 100, Kind: "text", Text: "hi"},
		{ChatJID: "a@s.whatsapp.net", ID: "2", FromMe: true, TS: 101, Kind: "text", Text: "hey"},
		{ChatJID: "a@s.whatsapp.net", ID: "3", SenderJID: "a@s.whatsapp.net", TS: 102, Kind: "text", Text: "dinner?"},
		{ChatJID: "g@g.us", ID: "4", SenderJID: "b@s.whatsapp.net", SenderName: "Bob", TS: 50, Kind: "text", Text: "yo"},
	} {
		added, err := s.AddMessage(ctx, m, true)
		must(err)
		if !added {
			t.Fatalf("message %d not added", i)
		}
	}
	if added, _ := s.AddMessage(ctx, Message{ChatJID: "g@g.us", ID: "4", TS: 50}, true); added {
		t.Fatal("duplicate added")
	}

	chats, err := s.ListChats(ctx, 1, 0, "", false)
	must(err)
	if len(chats) != 1 || chats[0].Name != "Alice" || chats[0].LastText != "dinner?" || chats[0].Unread != 1 {
		t.Fatalf("first page: %+v", chats)
	}
	chats, err = s.ListChats(ctx, 10, chats[0].LastTS, chats[0].JID, false)
	must(err)
	if len(chats) != 1 || chats[0].JID != "g@g.us" || !chats[0].Group || chats[0].Unread != 1 {
		t.Fatalf("second page: %+v", chats)
	}

	msgs, more, err := s.ListMessages(ctx, "a@s.whatsapp.net", 2, "")
	must(err)
	if !more || len(msgs) != 2 || msgs[0].ID != "2" || msgs[1].ID != "3" {
		t.Fatalf("newest page: more=%v %+v", more, msgs)
	}
	msgs, more, err = s.ListMessages(ctx, "a@s.whatsapp.net", 2, "2")
	must(err)
	if more || len(msgs) != 1 || msgs[0].ID != "1" {
		t.Fatalf("older page: more=%v %+v", more, msgs)
	}

	unseen, maxTS, err := s.UnseenIncoming(ctx, "a@s.whatsapp.net", "")
	must(err)
	if len(unseen["a@s.whatsapp.net"]) != 1 || maxTS != 102 {
		t.Fatalf("unseen: %v %d", unseen, maxTS)
	}
	must(s.MarkSeen(ctx, "a@s.whatsapp.net", maxTS))
	c, err := s.GetChat(ctx, "a@s.whatsapp.net")
	must(err)
	if c.Unread != 0 {
		t.Fatalf("unread after mark seen: %d", c.Unread)
	}

	must(s.SetStatus(ctx, "a@s.whatsapp.net", []string{"2"}, "read"))
	must(s.SetStatus(ctx, "a@s.whatsapp.net", []string{"2"}, "delivered"))
	msgs, _, _ = s.ListMessages(ctx, "a@s.whatsapp.net", 1, "3")
	if msgs[0].Status != "read" {
		t.Fatalf("status downgraded: %s", msgs[0].Status)
	}
}

func TestReplies(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	r, err := s.Replies(ctx)
	if err != nil || len(r) != len(DefaultReplies) {
		t.Fatalf("defaults: %v %v", r, err)
	}
	if err := s.SetReplies(ctx, []string{"a", " ", "b"}); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Replies(ctx)
	if len(r) != 2 || r[1] != "b" {
		t.Fatalf("got %v", r)
	}
}

func TestClientIDs(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if _, _, ok, _ := s.LookupClientID(ctx, "k1"); ok {
		t.Fatal("unexpected hit")
	}
	if err := s.RememberClientID(ctx, "k1", "a", "m1", 5); err != nil {
		t.Fatal(err)
	}
	id, ts, ok, err := s.LookupClientID(ctx, "k1")
	if !ok || err != nil || id != "m1" || ts != 5 {
		t.Fatalf("lookup: %s %d %v %v", id, ts, ok, err)
	}
}

func TestMediaAndGetMessage(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	s.AddMessage(ctx, Message{ChatJID: "a", ID: "1", TS: 1, Kind: "image", Text: "[Photo]"}, false)
	s.AddMessage(ctx, Message{ChatJID: "a", ID: "2", TS: 2, Kind: "text", Text: "long text"}, false)
	if err := s.SaveMedia(ctx, "a", "1", Media{Kind: "image", Proto: []byte{1}, Thumbnail: []byte{2}}); err != nil {
		t.Fatal(err)
	}
	msgs, _, err := s.ListMessages(ctx, "a", 10, "")
	if err != nil || len(msgs) != 2 || !msgs[0].HasImage || msgs[1].HasImage {
		t.Fatalf("list: %+v %v", msgs, err)
	}
	m, err := s.GetMessage(ctx, "a", "2")
	if err != nil || m.Text != "long text" {
		t.Fatalf("get: %+v %v", m, err)
	}
	if _, err := s.GetMessage(ctx, "a", "nope"); err != ErrNotFound {
		t.Fatalf("missing: %v", err)
	}
	media, err := s.GetMedia(ctx, "a", "1")
	if err != nil || media.Kind != "image" || media.Thumbnail[0] != 2 {
		t.Fatalf("media: %+v %v", media, err)
	}
}
