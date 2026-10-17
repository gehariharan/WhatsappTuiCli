package whatsapp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

const MaxAttachmentSize = 25 << 20

type SendInput struct {
	To   string `json:"to"`
	Text string `json:"text,omitempty"`
	File string `json:"file,omitempty"`
	Type string `json:"type,omitempty"`
}
type SendResult struct {
	ChatID         string    `json:"chat_id"`
	MessageID      string    `json:"message_id,omitempty"`
	Status         string    `json:"status"`
	Timestamp      time.Time `json:"timestamp,omitempty"`
	ArchiveWarning string    `json:"archive_warning,omitempty"`
}
type Sender func(context.Context, SendInput) (SendResult, error)

func (in SendInput) Validate() error {
	if _, err := ParseChat(in.To); err != nil {
		return err
	}
	if strings.TrimSpace(in.Text) == "" && in.File == "" {
		return fmt.Errorf("provide --text or --file")
	}
	if len(in.Text) > 16000 {
		return fmt.Errorf("text exceeds 16000 bytes")
	}
	if in.Type != "" && in.Type != "image" && in.Type != "document" {
		return fmt.Errorf("attachment type must be image or document")
	}
	if in.File == "" && in.Type != "" {
		return fmt.Errorf("attachment type requires --file")
	}
	return nil
}

// Send sends only an explicitly requested message; it does not run an application retry loop.
func (c *Client) Send(ctx context.Context, in SendInput) (SendResult, error) {
	out := SendResult{ChatID: in.To, Status: "not_sent"}
	if err := in.Validate(); err != nil {
		return out, err
	}
	if !c.Paired() || !c.client.IsConnected() {
		return out, fmt.Errorf("connect your paired device before sending")
	}
	jid, _ := ParseChat(in.To)
	message := &waE2E.Message{Conversation: proto.String(in.Text)}
	if in.File != "" {
		f, err := os.Open(in.File)
		if err != nil {
			return out, fmt.Errorf("open attachment: %w", err)
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > MaxAttachmentSize {
			f.Close()
			return out, fmt.Errorf("attachment must be a regular file of at most 25 MiB")
		}
		data, err := io.ReadAll(io.LimitReader(f, MaxAttachmentSize+1))
		f.Close()
		if err != nil || len(data) > MaxAttachmentSize {
			return out, fmt.Errorf("could not read attachment within 25 MiB limit")
		}
		mime := http.DetectContentType(data)
		mediaType := whatsmeow.MediaDocument
		if in.Type == "image" {
			if mime != "image/jpeg" && mime != "image/png" {
				return out, fmt.Errorf("image sending supports JPEG and PNG; use document for other files")
			}
			mediaType = whatsmeow.MediaImage
		}
		uploaded, err := c.client.Upload(ctx, data, mediaType)
		if err != nil {
			return out, fmt.Errorf("attachment upload failed; conversation message was not sent")
		}
		message = &waE2E.Message{}
		if mediaType == whatsmeow.MediaImage {
			message.ImageMessage = &waE2E.ImageMessage{Caption: proto.String(in.Text), Mimetype: proto.String(mime), URL: &uploaded.URL, DirectPath: &uploaded.DirectPath, MediaKey: uploaded.MediaKey, FileEncSHA256: uploaded.FileEncSHA256, FileSHA256: uploaded.FileSHA256, FileLength: &uploaded.FileLength}
		} else {
			message.DocumentMessage = &waE2E.DocumentMessage{Caption: proto.String(in.Text), FileName: proto.String(filepath.Base(in.File)), Mimetype: proto.String(mime), URL: &uploaded.URL, DirectPath: &uploaded.DirectPath, MediaKey: uploaded.MediaKey, FileEncSHA256: uploaded.FileEncSHA256, FileSHA256: uploaded.FileSHA256, FileLength: &uploaded.FileLength}
		}
	}
	out.MessageID = c.client.GenerateMessageID()
	out.Status = "unknown"
	response, err := c.client.SendMessage(ctx, jid, message, whatsmeow.SendRequestExtra{ID: out.MessageID, Timeout: 45 * time.Second, CheckPermissions: true})
	if err != nil {
		return out, fmt.Errorf("send outcome unknown; check WhatsApp before retrying; do not retry automatically")
	}
	out.Status = "server_acknowledged"
	out.Timestamp = response.Timestamp
	if c.capture != nil {
		event := &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: jid, Sender: c.client.Store.ID.ToNonAD(), IsFromMe: true, IsGroup: jid.Server == types.GroupServer}, ID: out.MessageID, Timestamp: response.Timestamp}, Message: message, RawMessage: message}
		if err = c.captureEvent(event); err != nil {
			out.ArchiveWarning = "Message acknowledged, but local archive failed; do not resend"
			c.StopCapture(err)
		}
	}
	return out, nil
}
