package wa

import (
	"context"
	"errors"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/repebble/repebble-whatsapp/server/internal/store"
)

// mediaFor extracts what's needed to show a message's picture later, or nil.
func mediaFor(m *waE2E.Message) *store.Media {
	var (
		kind  string
		msg   proto.Message
		thumb []byte
	)
	switch {
	case m.GetImageMessage() != nil:
		kind, msg, thumb = "image", m.GetImageMessage(), m.GetImageMessage().GetJPEGThumbnail()
	case m.GetStickerMessage() != nil:
		kind, msg, thumb = "sticker", m.GetStickerMessage(), m.GetStickerMessage().GetPngThumbnail()
	case m.GetVideoMessage() != nil:
		// We only ever show the poster frame, so no need to keep the download info.
		if t := m.GetVideoMessage().GetJPEGThumbnail(); len(t) > 0 {
			return &store.Media{Kind: "video", Thumbnail: t}
		}
		return nil
	default:
		return nil
	}
	raw, err := proto.Marshal(msg)
	if err != nil {
		return nil
	}
	return &store.Media{Kind: kind, Proto: raw, Thumbnail: thumb}
}

// FetchImage returns the original bytes of a message's picture. It downloads the
// full image when possible and falls back to the embedded thumbnail (e.g. when
// WhatsApp no longer has the file, or we're offline).
func (c *Client) FetchImage(ctx context.Context, chatJID, id string) ([]byte, error) {
	media, err := c.store.GetMedia(ctx, chatJID, id)
	if err != nil {
		return nil, err
	}
	data, dlErr := c.download(ctx, media)
	if dlErr == nil {
		return data, nil
	}
	if len(media.Thumbnail) > 0 {
		c.log.Warnf("Using thumbnail for %s/%s: %v", chatJID, id, dlErr)
		return media.Thumbnail, nil
	}
	return nil, dlErr
}

func (c *Client) download(ctx context.Context, media store.Media) ([]byte, error) {
	if len(media.Proto) == 0 {
		return nil, errors.New("no downloadable media")
	}
	cli, err := c.client()
	if err != nil {
		return nil, err
	}
	var msg interface {
		proto.Message
		whatsmeow.DownloadableMessage
	}
	switch media.Kind {
	case "image":
		msg = &waE2E.ImageMessage{}
	case "sticker":
		msg = &waE2E.StickerMessage{}
	default:
		return nil, fmt.Errorf("can't download %s", media.Kind)
	}
	if err := proto.Unmarshal(media.Proto, msg); err != nil {
		return nil, err
	}
	return cli.Download(ctx, msg)
}
