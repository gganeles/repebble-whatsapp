package wa

import (
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

// describe turns a WhatsApp message into a kind and display text. An empty kind
// means the message has nothing to show (reactions, key distribution, etc).
func describe(m *waE2E.Message) (kind, text string) {
	if m == nil {
		return "", ""
	}
	withCaption := func(label, caption string) string {
		if caption = strings.TrimSpace(caption); caption != "" {
			return label + " " + caption
		}
		return label
	}
	switch {
	case m.GetConversation() != "":
		return "text", m.GetConversation()
	case m.GetExtendedTextMessage() != nil:
		return "text", m.GetExtendedTextMessage().GetText()
	case m.GetImageMessage() != nil:
		return "image", withCaption("[Photo]", m.GetImageMessage().GetCaption())
	case m.GetVideoMessage() != nil:
		v := m.GetVideoMessage()
		if v.GetGifPlayback() {
			return "video", withCaption("[GIF]", v.GetCaption())
		}
		return "video", withCaption("[Video]", v.GetCaption())
	case m.GetAudioMessage() != nil:
		a := m.GetAudioMessage()
		secs := a.GetSeconds()
		dur := fmt.Sprintf("%d:%02d", secs/60, secs%60)
		if a.GetPTT() {
			return "voice", "[Voice note " + dur + "]"
		}
		return "audio", "[Audio " + dur + "]"
	case m.GetStickerMessage() != nil:
		return "sticker", "[Sticker]"
	case m.GetDocumentMessage() != nil:
		d := m.GetDocumentMessage()
		name := d.GetFileName()
		if name == "" {
			name = d.GetTitle()
		}
		label := "[Document]"
		if name != "" {
			label = "[Document: " + name + "]"
		}
		return "document", withCaption(label, d.GetCaption())
	case m.GetLocationMessage() != nil:
		l := m.GetLocationMessage()
		return "location", withCaption("[Location]", firstNonEmpty(l.GetName(), l.GetAddress()))
	case m.GetLiveLocationMessage() != nil:
		return "location", withCaption("[Live location]", m.GetLiveLocationMessage().GetCaption())
	case m.GetContactMessage() != nil:
		return "contact", withCaption("[Contact]", m.GetContactMessage().GetDisplayName())
	case m.GetContactsArrayMessage() != nil:
		return "contact", fmt.Sprintf("[%d contacts]", len(m.GetContactsArrayMessage().GetContacts()))
	case m.GetPollCreationMessage() != nil:
		return "other", withCaption("[Poll]", m.GetPollCreationMessage().GetName())
	case m.GetPollCreationMessageV3() != nil:
		return "other", withCaption("[Poll]", m.GetPollCreationMessageV3().GetName())
	case m.GetEventMessage() != nil:
		return "other", withCaption("[Event]", m.GetEventMessage().GetName())
	case m.GetGroupInviteMessage() != nil:
		return "other", withCaption("[Group invite]", m.GetGroupInviteMessage().GetGroupName())
	}
	return "", ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
