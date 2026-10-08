package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/repebble/repebble-whatsapp/server/internal/hub"
	"github.com/repebble/repebble-whatsapp/server/internal/store"
	"github.com/repebble/repebble-whatsapp/server/internal/wa"
	"github.com/repebble/repebble-whatsapp/server/internal/watchtext"
)

type fakeWA struct {
	st   *store.Store
	sent int
}

func (f *fakeWA) Status() wa.Status                            { return wa.Status{State: wa.StateConnected} }
func (f *fakeWA) Pair(context.Context, string) (string, error) { return "ABCD-EFGH", nil }
func (f *fakeWA) Logout(context.Context) error                 { return nil }
func (f *fakeWA) FetchImage(ctx context.Context, chat, id string) ([]byte, error) {
	if _, err := f.st.GetMedia(ctx, chat, id); err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, nil)
	return buf.Bytes(), nil
}
func (f *fakeWA) MarkRead(ctx context.Context, chat, upTo string) error {
	return f.st.SetUnread(ctx, chat, 0)
}
func (f *fakeWA) Send(ctx context.Context, chat, text string) (store.Message, error) {
	f.sent++
	m := store.Message{ChatJID: chat, ID: "sent1", FromMe: true, TS: 200, Kind: "text", Text: text, Status: "sent"}
	_, err := f.st.AddMessage(ctx, m, true)
	return m, err
}

func setup(t *testing.T) (*httptest.Server, *fakeWA) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.UpsertChat(ctx, "1@s.whatsapp.net", "Alice Example", false)
	st.UpsertChat(ctx, "g@g.us", "Climbing crew", true)
	st.AddMessage(ctx, store.Message{ChatJID: "1@s.whatsapp.net", ID: "a", SenderJID: "1@s.whatsapp.net", TS: 100, Kind: "text", Text: "dinner at 8? 👍"}, true)
	st.AddMessage(ctx, store.Message{ChatJID: "g@g.us", ID: "b", SenderJID: "2@s.whatsapp.net", SenderName: "Bob Builder", TS: 90, Kind: "image", Text: "[Photo] " + strings.Repeat("long caption ", 40)}, true)
	st.SaveMedia(ctx, "g@g.us", "b", store.Media{Kind: "image"})
	f := &fakeWA{st: st}
	srv := httptest.NewServer(New(f, st, hub.New(), "secret", watchtext.Options{}))
	t.Cleanup(srv.Close)
	return srv, f
}

func do(t *testing.T, srv *httptest.Server, method, path, body string, out any) int {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func TestAuth(t *testing.T) {
	srv, _ := setup(t)
	resp, err := http.Get(srv.URL + "/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("got %d", resp.StatusCode)
	}
	resp, _ = http.Get(srv.URL + "/v1/status?token=secret")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("query token: got %d", resp.StatusCode)
	}
}

func TestChatsPaging(t *testing.T) {
	srv, _ := setup(t)
	var page struct {
		Chats []chatJSON `json:"chats"`
		Next  *string    `json:"next"`
	}
	do(t, srv, "GET", "/v1/chats?limit=1", "", &page)
	if len(page.Chats) != 1 || page.Next == nil || page.Chats[0].Preview != "dinner at 8? (y)" || page.Chats[0].Unread != 1 {
		t.Fatalf("page 1: %+v", page)
	}
	cur := *page.Next
	page.Next = nil
	do(t, srv, "GET", "/v1/chats?limit=1&before="+cur, "", &page)
	c := page.Chats[0]
	if page.Next != nil || c.Name != "Climbing crew" || !c.Group || !strings.HasPrefix(c.Preview, "Bob: [Photo]") || len(c.Preview) > watchtext.Preview {
		t.Fatalf("page 2: %+v", page)
	}
}

func TestSendIdempotentAndRead(t *testing.T) {
	srv, f := setup(t)
	for i := 0; i < 2; i++ {
		var out map[string]any
		if code := do(t, srv, "POST", "/v1/chats/1@s.whatsapp.net/messages", `{"text":"On my way","client_id":"k1"}`, &out); code != 200 || out["id"] != "sent1" {
			t.Fatalf("send %d: %d %v", i, code, out)
		}
	}
	if f.sent != 1 {
		t.Fatalf("sent %d times", f.sent)
	}
	var msgs struct {
		Messages []messageJSON `json:"messages"`
	}
	do(t, srv, "GET", "/v1/chats/1@s.whatsapp.net/messages", "", &msgs)
	if len(msgs.Messages) != 2 || msgs.Messages[1].Text != "On my way" || !msgs.Messages[1].FromMe {
		t.Fatalf("messages: %+v", msgs)
	}
	if code := do(t, srv, "POST", "/v1/chats/1@s.whatsapp.net/read", `{}`, nil); code != 204 {
		t.Fatalf("read: %d", code)
	}
	if code := do(t, srv, "POST", "/v1/chats/1@s.whatsapp.net/messages", `{"text":"  "}`, nil); code != 400 {
		t.Fatalf("empty send: %d", code)
	}
}

func TestReplies(t *testing.T) {
	srv, _ := setup(t)
	var out struct {
		Replies []string `json:"replies"`
	}
	do(t, srv, "PUT", "/v1/replies", `{"replies":["Yes 👍","No"]}`, &out)
	if len(out.Replies) != 2 || out.Replies[0] != "Yes (y)" {
		t.Fatalf("replies: %v", out.Replies)
	}
}

func TestFullTextAndImage(t *testing.T) {
	srv, _ := setup(t)
	var list struct {
		Messages []messageJSON `json:"messages"`
	}
	do(t, srv, "GET", "/v1/chats/g@g.us/messages", "", &list)
	m := list.Messages[0]
	if !m.Truncated || !m.HasImage || len(m.Text) > watchtext.MessageText {
		t.Fatalf("list item: %+v", m)
	}
	var full struct {
		Message messageJSON `json:"message"`
	}
	do(t, srv, "GET", "/v1/chats/g@g.us/messages/b", "", &full)
	if full.Message.Truncated || len(full.Message.Text) <= watchtext.MessageText {
		t.Fatalf("full: truncated=%v len=%d", full.Message.Truncated, len(full.Message.Text))
	}
	var img struct {
		Width    int    `json:"width"`
		Height   int    `json:"height"`
		Format   string `json:"format"`
		RowBytes int    `json:"row_bytes"`
		Data     string `json:"data"`
	}
	if code := do(t, srv, "GET", "/v1/chats/g@g.us/messages/b/image?w=192&h=192", "", &img); code != 200 {
		t.Fatalf("image: %d", code)
	}
	raw, _ := base64.StdEncoding.DecodeString(img.Data)
	if img.Width != 192 || img.Height != 144 || img.Format != "color" || len(raw) != 192*144 {
		t.Fatalf("image: %dx%d %s %d bytes", img.Width, img.Height, img.Format, len(raw))
	}
	if code := do(t, srv, "GET", "/v1/chats/1@s.whatsapp.net/messages/a/image", "", nil); code != 404 {
		t.Fatalf("no media: %d", code)
	}
}
