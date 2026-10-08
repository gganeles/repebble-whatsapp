package wa

import (
	"context"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/repebble/repebble-whatsapp/server/internal/hub"
	"github.com/repebble/repebble-whatsapp/server/internal/store"
)

func (c *Client) handleEvent(raw any) {
	ctx := context.Background()
	switch evt := raw.(type) {
	case *events.Connected:
		c.setState(StateConnected)
	case *events.Disconnected:
		if c.State() == StateConnected {
			c.setState(StateConnecting)
		}
	case *events.PairSuccess:
		c.log.Infof("Paired as %s (%s)", evt.ID, evt.Platform)
		c.mu.Lock()
		c.qr, c.pairingCode = "", ""
		c.mu.Unlock()
		c.setState(StateConnecting)
	case *events.LoggedOut:
		c.log.Warnf("Logged out by WhatsApp (reason %v)", evt.Reason)
		c.resetAfterLogout()
	case *events.StreamReplaced:
		c.log.Warnf("Another client took over this session")
		c.setState(StateConnecting)
	case *events.Message:
		c.handleMessage(ctx, evt, true)
	case *events.Receipt:
		c.handleReceipt(ctx, evt)
	case *events.HistorySync:
		c.handleHistorySync(ctx, evt.Data)
	case *events.Archive:
		chat := c.normalizeChat(ctx, evt.JID).String()
		_ = c.store.SetArchived(ctx, chat, evt.Action.GetArchived())
		c.publishChat(ctx, chat)
	case *events.Mute:
		chat := c.normalizeChat(ctx, evt.JID).String()
		_ = c.store.SetMuted(ctx, chat, evt.Action.GetMuted())
		c.publishChat(ctx, chat)
	case *events.MarkChatAsRead:
		chat := c.normalizeChat(ctx, evt.JID).String()
		if evt.Action.GetRead() {
			_ = c.store.SetUnread(ctx, chat, 0)
			c.publishChat(ctx, chat)
		}
	}
}

func (c *Client) handleMessage(ctx context.Context, evt *events.Message, live bool) {
	info := evt.Info
	if info.Chat == types.StatusBroadcastJID || info.Chat.Server == types.NewsletterServer || info.Chat.Server == types.BroadcastServer {
		return
	}
	chat := c.normalizeChat(ctx, info.Chat)
	chatJID := chat.String()

	if pm := evt.Message.GetProtocolMessage(); pm != nil {
		target := pm.GetKey().GetID()
		var text string
		switch pm.GetType() {
		case waE2E.ProtocolMessage_REVOKE:
			text = "[Deleted]"
		case waE2E.ProtocolMessage_MESSAGE_EDIT:
			_, text = describe(pm.GetEditedMessage())
		}
		if target != "" && text != "" {
			if ok, _ := c.store.EditMessage(ctx, chatJID, target, text); ok && live {
				c.publishChat(ctx, chatJID)
			}
		}
		return
	}

	kind, text := describe(evt.Message)
	if kind == "" {
		return
	}

	isGroup := chat.Server == types.GroupServer
	name := ""
	if !isGroup && !info.IsFromMe {
		name = c.contactName(ctx, chat, info.PushName)
	} else if !isGroup {
		name = c.contactName(ctx, chat, "")
	}
	if err := c.store.UpsertChat(ctx, chatJID, name, isGroup); err != nil {
		c.log.Errorf("Upsert chat %s: %v", chatJID, err)
		return
	}
	if isGroup && live {
		c.ensureGroupName(ctx, chat)
	}

	m := store.Message{
		ChatJID: chatJID, ID: info.ID, FromMe: info.IsFromMe,
		TS: info.Timestamp.Unix(), Kind: kind, Text: text, Status: "sent",
	}
	if !info.IsFromMe {
		m.SenderJID = info.Sender.ToNonAD().String()
		if isGroup {
			m.SenderName = c.contactName(ctx, info.Sender, info.PushName)
		}
	}
	added, err := c.store.AddMessage(ctx, m, live)
	if err != nil {
		c.log.Errorf("Store message %s: %v", info.ID, err)
		return
	}
	if added {
		if media := mediaFor(evt.Message); media != nil {
			if err := c.store.SaveMedia(ctx, chatJID, info.ID, *media); err != nil {
				c.log.Warnf("Store media %s: %v", info.ID, err)
			} else {
				m.HasImage = true
			}
		}
	}
	if added && live {
		c.publishMessage(ctx, m)
	}
}

func (c *Client) ensureGroupName(ctx context.Context, jid types.JID) {
	c.mu.Lock()
	done := c.groupNames[jid]
	c.groupNames[jid] = true
	cli := c.cli
	c.mu.Unlock()
	if done {
		return
	}
	if name, _ := c.store.ChatName(ctx, jid.String()); name != "" {
		return
	}
	go func() {
		info, err := cli.GetGroupInfo(context.Background(), jid)
		if err != nil {
			c.log.Warnf("Group info %s: %v", jid, err)
			return
		}
		_ = c.store.SetChatName(context.Background(), jid.String(), info.Name)
		c.publishChat(context.Background(), jid.String())
	}()
}

func (c *Client) handleReceipt(ctx context.Context, evt *events.Receipt) {
	chat := c.normalizeChat(ctx, evt.Chat).String()
	switch evt.Type {
	case types.ReceiptTypeDelivered, types.ReceiptTypeRead, types.ReceiptTypePlayed:
		if evt.IsFromMe {
			return
		}
		status := map[types.ReceiptType]string{
			types.ReceiptTypeDelivered: "delivered",
			types.ReceiptTypeRead:      "read",
			types.ReceiptTypePlayed:    "read",
		}[evt.Type]
		if err := c.store.SetStatus(ctx, chat, evt.MessageIDs, status); err == nil {
			c.hub.Publish(hub.Event{Type: "receipt", Chat: chat, IDs: evt.MessageIDs, Status: status})
		}
	case types.ReceiptTypeReadSelf:
		// Read on the phone or another linked device.
		var maxTS int64
		for _, id := range evt.MessageIDs {
			if ts, err := c.store.MessageTS(ctx, chat, id); err == nil {
				maxTS = max(maxTS, ts)
			}
		}
		if maxTS == 0 {
			return
		}
		if err := c.store.MarkSeen(ctx, chat, maxTS); err == nil {
			c.publishChat(ctx, chat)
		}
	}
}

func (c *Client) handleHistorySync(ctx context.Context, data *waHistorySync.HistorySync) {
	c.mu.Lock()
	c.historySync = "running"
	cli := c.cli
	c.mu.Unlock()
	convs := data.GetConversations()
	c.log.Infof("History sync %s: %d conversations", data.GetSyncType(), len(convs))
	for _, conv := range convs {
		rawJID, err := types.ParseJID(conv.GetID())
		if err != nil {
			continue
		}
		if rawJID == types.StatusBroadcastJID || rawJID.Server == types.NewsletterServer || rawJID.Server == types.BroadcastServer {
			continue
		}
		chat := c.normalizeChat(ctx, rawJID)
		isGroup := chat.Server == types.GroupServer
		name := firstNonEmpty(conv.GetName(), conv.GetDisplayName())
		if name == "" && !isGroup {
			name = c.contactName(ctx, chat, "")
		}
		if err := c.store.UpsertChat(ctx, chat.String(), name, isGroup); err != nil {
			c.log.Errorf("History chat %s: %v", chat, err)
			continue
		}
		for _, hm := range conv.GetMessages() {
			evt, err := cli.ParseWebMessage(rawJID, hm.GetMessage())
			if err != nil {
				continue
			}
			c.handleMessage(ctx, evt, false)
		}
		_ = c.store.SetUnread(ctx, chat.String(), int(conv.GetUnreadCount()))
		if conv.GetArchived() {
			_ = c.store.SetArchived(ctx, chat.String(), true)
		}
	}
	c.mu.Lock()
	c.historySync = "done"
	c.mu.Unlock()
	c.hub.Publish(hub.Event{Type: "connection", State: string(c.State())})
}
