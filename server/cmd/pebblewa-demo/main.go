// pebblewa-demo serves the same API as pebblewa with made-up chats and no WhatsApp
// connection, for developing the watchapp in the emulator. Contacts answer your
// messages after a couple of seconds so live updates can be tested.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/repebble/repebble-whatsapp/server/internal/api"
	"github.com/repebble/repebble-whatsapp/server/internal/hub"
	"github.com/repebble/repebble-whatsapp/server/internal/store"
	"github.com/repebble/repebble-whatsapp/server/internal/wa"
	"github.com/repebble/repebble-whatsapp/server/internal/watchtext"
)

type demoWA struct {
	st  *store.Store
	hub *hub.Hub
	mu  sync.Mutex
	seq int
}

func (d *demoWA) Status() wa.Status {
	return wa.Status{State: wa.StateConnected, Phone: "+10000000000", PushName: "Demo", HistorySync: "done"}
}
func (d *demoWA) Pair(context.Context, string) (string, error) { return "DEMO-CODE", nil }
func (d *demoWA) Logout(context.Context) error                 { return nil }

// FetchImage draws a sunset so image transfer can be tested without WhatsApp.
func (d *demoWA) FetchImage(ctx context.Context, chat, id string) ([]byte, error) {
	if _, err := d.st.GetMedia(ctx, chat, id); err != nil {
		return nil, err
	}
	const w, h = 640, 480
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{uint8(255 - y*120/h), uint8(80 + y*100/h), uint8(60 + y*190/h), 255}
			if dx, dy := x-w/2, y-h*2/3; dx*dx+dy*dy < 90*90 && y < h*2/3 {
				c = color.RGBA{255, 210, 60, 255} // sun
			}
			if y > h*2/3 {
				c = color.RGBA{20, uint8(60 + (y-h*2/3)/4), 110, 255} // sea
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (d *demoWA) MarkRead(ctx context.Context, chat, upTo string) error {
	if err := d.st.SetUnread(ctx, chat, 0); err != nil {
		return err
	}
	d.publishChat(ctx, chat)
	return nil
}

func (d *demoWA) nextID() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seq++
	return fmt.Sprintf("DEMO%06d", d.seq)
}

func (d *demoWA) add(ctx context.Context, m store.Message) {
	if _, err := d.st.AddMessage(ctx, m, true); err != nil {
		fmt.Fprintln(os.Stderr, "add:", err)
		return
	}
	mm := m
	d.hub.Publish(hub.Event{Type: "message", Chat: m.ChatJID, Message: &mm})
	d.publishChat(ctx, m.ChatJID)
}

func (d *demoWA) publishChat(ctx context.Context, jid string) {
	if c, err := d.st.GetChat(ctx, jid); err == nil {
		d.hub.Publish(hub.Event{Type: "chat", Chat: jid, ChatRow: &c})
	}
}

func (d *demoWA) Send(ctx context.Context, chat, text string) (store.Message, error) {
	m := store.Message{ChatJID: chat, ID: d.nextID(), FromMe: true, TS: time.Now().Unix(), Kind: "text", Text: text, Status: "sent"}
	d.add(ctx, m)
	go func() {
		time.Sleep(2 * time.Second)
		bg := context.Background()
		_ = d.st.SetStatus(bg, chat, []string{m.ID}, "read")
		d.hub.Publish(hub.Event{Type: "receipt", Chat: chat, IDs: []string{m.ID}, Status: "read"})
		reply := store.Message{ChatJID: chat, ID: d.nextID(), SenderJID: chat, TS: time.Now().Unix(), Kind: "text", Text: "Got it: " + text}
		if strings.HasSuffix(chat, "@g.us") {
			reply.SenderJID, reply.SenderName = "15550001111@s.whatsapp.net", "Sam Rivera"
		}
		d.add(bg, reply)
	}()
	return m, nil
}

func seed(ctx context.Context, st *store.Store, d *demoWA) error {
	now := time.Now().Unix()
	type msg struct {
		ago    int64
		fromMe bool
		sender string
		kind   string
		text   string
	}
	chats := []struct {
		jid, name string
		group     bool
		msgs      []msg
	}{
		{"15551230001@s.whatsapp.net", "Alice Martin", false, []msg{
			{7200, false, "", "text", "Are we still on for tonight?"},
			{7100, true, "", "text", "Yes! 8pm at the usual place 👍"},
			{300, false, "", "text", "Great, I'll book a table. Can you bring the charger you borrowed last week?"},
			{250, false, "", "text", "Also, long story about the weekend: " + strings.Repeat("we drove up the coast, got lost twice, found an amazing bakery, and then the car battery died right outside the campsite. ", 6) + "Anyway, tell you the rest tonight!"},
			{120, false, "", "image", "[Photo] the menu"},
		}},
		{"120363000000000001@g.us", "Climbing crew", true, []msg{
			{90000, false, "Sam Rivera", "text", "Who's in for Saturday?"},
			{86000, true, "", "text", "Me"},
			{3600, false, "Jordan Lee", "voice", "[Voice note 0:12]"},
			{1800, false, "Sam Rivera", "text", "Meeting at 9 at the gym 🧗"},
		}},
		{"15551230002@s.whatsapp.net", "Mom", false, []msg{
			{200000, false, "", "text", "Call me when you can ❤️"},
			{190000, true, "", "text", "Will do, after work"},
		}},
	}
	for i := 0; i < 20; i++ {
		chats = append(chats, struct {
			jid, name string
			group     bool
			msgs      []msg
		}{fmt.Sprintf("1555999%04d@s.whatsapp.net", i), fmt.Sprintf("Contact %d", i+1), false,
			[]msg{{int64(300000 + i*5000), i%2 == 0, "", "text", fmt.Sprintf("Old message number %d", i+1)}}})
	}
	// A long chat to exercise paging.
	long := chats[0]
	for i := 0; i < 40; i++ {
		long.msgs = append(long.msgs, msg{int64(100000 - i*100), i%3 == 0, "", "text", fmt.Sprintf("History message %d", i+1)})
	}
	chats[0] = long
	for _, c := range chats {
		if err := st.UpsertChat(ctx, c.jid, c.name, c.group); err != nil {
			return err
		}
		for _, m := range c.msgs {
			sm := store.Message{ChatJID: c.jid, ID: d.nextID(), FromMe: m.fromMe, TS: now - m.ago, Kind: m.kind, Text: m.text, Status: "read"}
			if !m.fromMe {
				sm.SenderJID, sm.SenderName = c.jid, m.sender
			}
			if _, err := st.AddMessage(ctx, sm, !m.fromMe && m.ago < 4000); err != nil {
				return err
			}
			if m.kind == "image" {
				if err := st.SaveMedia(ctx, c.jid, sm.ID, store.Media{Kind: "image"}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8723", "listen address")
	token := flag.String("token", "demo", "API token")
	flag.Parse()

	ctx := context.Background()
	dir, err := os.MkdirTemp("", "pebblewa-demo")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	st, err := store.Open(ctx, filepath.Join(dir, "app.db"))
	if err != nil {
		panic(err)
	}
	h := hub.New()
	d := &demoWA{st: st, hub: h}
	if err := seed(ctx, st, d); err != nil {
		panic(err)
	}
	fmt.Printf("Demo API on http://%s (token: %s)\n", *addr, *token)
	if err := http.ListenAndServe(*addr, api.New(d, st, h, *token, watchtext.Options{})); err != nil {
		panic(err)
	}
}
