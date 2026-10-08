// Package wa wraps whatsmeow: pairing, staying connected, and mirroring
// chats and messages into the store.
package wa

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/repebble/repebble-whatsapp/server/internal/hub"
	"github.com/repebble/repebble-whatsapp/server/internal/store"
)

type State string

const (
	StateUnpaired   State = "unpaired"
	StatePairing    State = "pairing"
	StateConnecting State = "connecting"
	StateConnected  State = "connected"
	StateLoggedOut  State = "logged_out"
)

type Status struct {
	State       State  `json:"state"`
	Phone       string `json:"phone,omitempty"`
	PushName    string `json:"push_name,omitempty"`
	PairingCode string `json:"pairing_code,omitempty"`
	QR          string `json:"qr,omitempty"`
	HistorySync string `json:"history_sync,omitempty"`
}

var (
	ErrNotPaired     = errors.New("not paired")
	ErrAlreadyPaired = errors.New("already paired")
	ErrDisconnected  = errors.New("not connected to WhatsApp")
)

type Client struct {
	container *sqlstore.Container
	store     *store.Store
	hub       *hub.Hub
	log       waLog.Logger

	mu          sync.Mutex
	cli         *whatsmeow.Client
	state       State
	pairingCode string
	qr          string
	historySync string
	groupNames  map[types.JID]bool // groups whose name we've already fetched this run
}

// New opens the whatsmeow session store at dbPath.
func New(ctx context.Context, dbPath string, st *store.Store, h *hub.Hub, log waLog.Logger) (*Client, error) {
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", dbPath))
	if err != nil {
		return nil, err
	}
	container := sqlstore.NewWithDB(db, "sqlite3", log.Sub("Store"))
	if err := container.Upgrade(ctx); err != nil {
		return nil, fmt.Errorf("upgrade whatsmeow store: %w", err)
	}
	return &Client{container: container, store: st, hub: h, log: log, groupNames: map[types.JID]bool{}}, nil
}

// Start connects to WhatsApp. If the device isn't paired yet, it connects in
// pairing mode so a QR code is available and Pair can request a code.
func (c *Client) Start(ctx context.Context) error {
	device, err := c.container.GetFirstDevice(ctx)
	if err != nil {
		return err
	}
	cli := whatsmeow.NewClient(device, c.log.Sub("Client"))
	cli.AddEventHandler(c.handleEvent)
	c.mu.Lock()
	c.cli = cli
	c.mu.Unlock()
	if device.ID == nil {
		if err := c.connectForPairing(ctx); err != nil {
			// Pair() retries the connection, so stay up and report unpaired.
			c.log.Warnf("Connect for pairing failed: %v", err)
		}
		return nil
	}
	c.setState(StateConnecting)
	go c.connectWithRetry(cli)
	return nil
}

// connectWithRetry keeps trying the first connection (e.g. phone offline at boot).
// After that whatsmeow's auto-reconnect takes over.
func (c *Client) connectWithRetry(cli *whatsmeow.Client) {
	delay := 2 * time.Second
	for {
		err := cli.Connect()
		if err == nil || errors.Is(err, whatsmeow.ErrAlreadyConnected) {
			return
		}
		c.log.Warnf("Connect failed, retrying in %s: %v", delay, err)
		time.Sleep(delay)
		delay = min(delay*2, 2*time.Minute)
	}
}

func (c *Client) connectForPairing(ctx context.Context) error {
	c.mu.Lock()
	cli := c.cli
	c.mu.Unlock()
	if cli.IsConnected() {
		return nil
	}
	qrCh, err := cli.GetQRChannel(context.Background())
	if err != nil {
		return err
	}
	c.setState(StateUnpaired)
	go func() {
		for item := range qrCh {
			switch item.Event {
			case whatsmeow.QRChannelEventCode:
				c.mu.Lock()
				c.qr = item.Code
				c.mu.Unlock()
			case whatsmeow.QRChannelSuccess.Event:
				c.log.Infof("Pairing successful")
			default:
				// Timeout or error: the socket is closed. Pair() reconnects on demand.
				c.log.Warnf("Pairing channel ended: %s %v", item.Event, item.Error)
				c.mu.Lock()
				c.qr, c.pairingCode = "", ""
				c.mu.Unlock()
				if c.State() == StatePairing {
					c.setState(StateUnpaired)
				}
			}
		}
	}()
	return cli.Connect()
}

// Pair requests an 8-character phone-number pairing code.
func (c *Client) Pair(ctx context.Context, phone string) (string, error) {
	c.mu.Lock()
	cli := c.cli
	c.mu.Unlock()
	if cli.Store.ID != nil {
		return "", ErrAlreadyPaired
	}
	if !cli.IsConnected() {
		if err := c.connectForPairing(ctx); err != nil {
			return "", err
		}
		// Give the websocket a moment to finish the handshake.
		for i := 0; i < 50 && !cli.IsConnected(); i++ {
			time.Sleep(100 * time.Millisecond)
		}
	}
	code, err := cli.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.pairingCode = code
	c.mu.Unlock()
	c.setState(StatePairing)
	return code, nil
}

func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Status{State: c.state, HistorySync: c.historySync}
	if c.cli != nil && c.cli.Store.ID != nil {
		s.Phone = "+" + c.cli.Store.ID.User
		s.PushName = c.cli.Store.PushName
	}
	if c.state == StateUnpaired || c.state == StatePairing {
		s.QR = c.qr
		s.PairingCode = c.pairingCode
	}
	return s
}

func (c *Client) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *Client) setState(s State) {
	c.mu.Lock()
	changed := c.state != s
	c.state = s
	c.mu.Unlock()
	if changed {
		c.log.Infof("State: %s", s)
		c.hub.Publish(hub.Event{Type: "connection", State: string(s)})
	}
}

func (c *Client) client() (*whatsmeow.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cli == nil || c.cli.Store.ID == nil {
		return nil, ErrNotPaired
	}
	if !c.cli.IsConnected() {
		return nil, ErrDisconnected
	}
	return c.cli, nil
}

// Send sends a text message and stores it.
func (c *Client) Send(ctx context.Context, chatJID, text string) (store.Message, error) {
	cli, err := c.client()
	if err != nil {
		return store.Message{}, err
	}
	jid, err := types.ParseJID(chatJID)
	if err != nil {
		return store.Message{}, fmt.Errorf("bad chat id: %w", err)
	}
	resp, err := cli.SendMessage(ctx, jid, &waE2E.Message{Conversation: proto.String(text)})
	if err != nil {
		return store.Message{}, err
	}
	m := store.Message{
		ChatJID: chatJID, ID: resp.ID, FromMe: true, TS: resp.Timestamp.Unix(),
		Kind: "text", Text: text, Status: "sent",
	}
	if _, err := c.store.AddMessage(ctx, m, true); err != nil {
		return m, err
	}
	c.publishMessage(ctx, m)
	return m, nil
}

// MarkRead sends read receipts for unread incoming messages up to upToID (all if empty).
func (c *Client) MarkRead(ctx context.Context, chatJID, upToID string) error {
	cli, err := c.client()
	if err != nil {
		return err
	}
	chat, err := types.ParseJID(chatJID)
	if err != nil {
		return err
	}
	bySender, maxTS, err := c.store.UnseenIncoming(ctx, chatJID, upToID)
	if err != nil || len(bySender) == 0 {
		if err == nil {
			err = c.store.SetUnread(ctx, chatJID, 0)
		}
		return err
	}
	for sender, ids := range bySender {
		senderJID, _ := types.ParseJID(sender)
		if err := cli.MarkRead(ctx, ids, time.Now(), chat, senderJID); err != nil {
			return err
		}
	}
	if err := c.store.MarkSeen(ctx, chatJID, maxTS); err != nil {
		return err
	}
	c.publishChat(ctx, chatJID)
	return nil
}

func (c *Client) Logout(ctx context.Context) error {
	cli, err := c.client()
	if err != nil {
		return err
	}
	return cli.Logout(ctx)
}

func (c *Client) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cli != nil {
		c.cli.Disconnect()
	}
}

// resetAfterLogout replaces the client with a fresh, unpaired device.
func (c *Client) resetAfterLogout() {
	c.mu.Lock()
	old := c.cli
	c.qr, c.pairingCode = "", ""
	c.mu.Unlock()
	if old != nil {
		old.Disconnect()
	}
	c.setState(StateLoggedOut)
	go func() {
		time.Sleep(time.Second)
		if err := c.Start(context.Background()); err != nil {
			c.log.Errorf("Restart after logout failed: %v", err)
		}
	}()
}

// normalizeChat maps hidden-user (LID) chat ids to phone-number ids when known, so a
// person has one chat regardless of how WhatsApp addressed the message.
func (c *Client) normalizeChat(ctx context.Context, jid types.JID) types.JID {
	jid = jid.ToNonAD()
	if jid.Server == types.HiddenUserServer {
		c.mu.Lock()
		cli := c.cli
		c.mu.Unlock()
		if pn, err := cli.Store.LIDs.GetPNForLID(ctx, jid); err == nil && !pn.IsEmpty() {
			return pn.ToNonAD()
		}
	}
	return jid
}

// contactName picks the best display name for a user.
func (c *Client) contactName(ctx context.Context, jid types.JID, pushName string) string {
	c.mu.Lock()
	cli := c.cli
	c.mu.Unlock()
	if info, err := cli.Store.Contacts.GetContact(ctx, jid.ToNonAD()); err == nil && info.Found {
		if n := firstNonEmpty(info.FullName, info.FirstName, info.BusinessName, info.PushName); n != "" {
			return n
		}
	}
	if pushName != "" {
		return pushName
	}
	if jid.Server == types.DefaultUserServer {
		return "+" + jid.User
	}
	if info, err := cli.Store.Contacts.GetContact(ctx, jid.ToNonAD()); err == nil && info.RedactedPhone != "" {
		return info.RedactedPhone
	}
	return jid.User
}

func (c *Client) publishMessage(ctx context.Context, m store.Message) {
	mm := m
	c.hub.Publish(hub.Event{Type: "message", Chat: m.ChatJID, Message: &mm})
	c.publishChat(ctx, m.ChatJID)
}

func (c *Client) publishChat(ctx context.Context, chatJID string) {
	if ch, err := c.store.GetChat(ctx, chatJID); err == nil {
		c.hub.Publish(hub.Event{Type: "chat", Chat: chatJID, ChatRow: &ch})
	}
}
