package whatsapp

import (
	"encoding/json"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"whatsappsync/internal/storage"
)

// unwrap examines wrappers without retaining raw protobufs in our archive.
func unwrap(m *waE2E.Message) *waE2E.Message {
	for range 12 {
		var next *waE2E.Message
		switch {
		case m.GetDeviceSentMessage() != nil:
			next = m.GetDeviceSentMessage().GetMessage()
		case m.GetEphemeralMessage() != nil:
			next = m.GetEphemeralMessage().GetMessage()
		case m.GetEditedMessage() != nil:
			next = m.GetEditedMessage().GetMessage()
		case m.GetDocumentWithCaptionMessage() != nil:
			next = m.GetDocumentWithCaptionMessage().GetMessage()
		default:
			return m
		}
		if next == nil {
			return m
		}
		m = next
	}
	return nil
}

// Normalize deliberately excludes quoted bodies, thumbnails, download URLs and
// media decryption keys. The original sender and group identifiers remain opaque.
func Normalize(e *events.Message, source string, now time.Time) (storage.Message, bool) {
	if e == nil || e.Info.Chat.Server != types.GroupServer || e.Info.ID == "" || e.Message == nil {
		return storage.Message{}, false
	}
	out := storage.Message{GroupID: e.Info.Chat.String(), MessageID: e.Info.ID, SenderID: e.Info.Sender.String(), Timestamp: e.Info.Timestamp, FromMe: e.Info.IsFromMe, Source: source, Type: "unknown"}
	if !e.Info.SenderAlt.IsEmpty() {
		out.SenderAlt = e.Info.SenderAlt.String()
	}
	m := e.Message
	raw := unwrap(e.RawMessage)
	protocol := raw.GetProtocolMessage()
	if protocol == nil {
		protocol = m.GetProtocolMessage()
	}
	if protocol != nil {
		// Explicit type presence avoids treating an unrelated empty protocol message as a revoke.
		if protocol.Type == nil {
			return out, false
		}
		if remote := protocol.GetKey().GetRemoteJID(); remote != "" && remote != out.GroupID {
			return out, false
		}
		switch protocol.GetType() {
		case waE2E.ProtocolMessage_REVOKE:
			out.Kind = "delete"
			out.MessageID = protocol.GetKey().GetID()
			out.Type = "deleted"
			out.MutationAt = e.Info.Timestamp
			if protocol.GetTimestampMS() > 0 {
				out.MutationAt = time.UnixMilli(protocol.GetTimestampMS())
			}
			return out, out.MessageID != "" && !out.MutationAt.IsZero()
		case waE2E.ProtocolMessage_MESSAGE_EDIT:
			out.Kind = "edit"
			out.MessageID = protocol.GetKey().GetID()
			out.MutationAt = e.Info.Timestamp
			if protocol.GetTimestampMS() > 0 {
				out.MutationAt = time.UnixMilli(protocol.GetTimestampMS())
			}
			m = protocol.GetEditedMessage()
			if m == nil || out.MessageID == "" {
				return out, false
			}
		default:
			return out, false
		}
	} else if e.IsEdit {
		// A wrapper with no explicit target is ambiguous: do not overwrite an original.
		return out, false
	}
	if e.IsViewOnce || e.IsViewOnceV2 || e.IsViewOnceV2Extension {
		return out, false
	}
	var contextInfo *waE2E.ContextInfo
	type mediaInfo struct {
		MIME     string `json:"mime,omitempty"`
		Filename string `json:"filename,omitempty"`
		Size     uint64 `json:"size,omitempty"`
		Width    uint32 `json:"width,omitempty"`
		Height   uint32 `json:"height,omitempty"`
		Duration uint32 `json:"duration_seconds,omitempty"`
	}
	var metadata *mediaInfo
	switch {
	case m.GetConversation() != "":
		out.Type = "text"
		out.Text = m.GetConversation()
	case m.GetExtendedTextMessage() != nil:
		v := m.GetExtendedTextMessage()
		out.Type = "text"
		out.Text = v.GetText()
		contextInfo = v.GetContextInfo()
	case m.GetImageMessage() != nil:
		v := m.GetImageMessage()
		out.Type = "image"
		out.Text = v.GetCaption()
		contextInfo = v.GetContextInfo()
		metadata = &mediaInfo{MIME: v.GetMimetype(), Size: v.GetFileLength(), Width: v.GetWidth(), Height: v.GetHeight()}
	case m.GetVideoMessage() != nil:
		v := m.GetVideoMessage()
		out.Type = "video"
		out.Text = v.GetCaption()
		contextInfo = v.GetContextInfo()
		metadata = &mediaInfo{MIME: v.GetMimetype(), Size: v.GetFileLength(), Width: v.GetWidth(), Height: v.GetHeight(), Duration: v.GetSeconds()}
	case m.GetDocumentMessage() != nil:
		v := m.GetDocumentMessage()
		out.Type = "document"
		out.Text = v.GetCaption()
		contextInfo = v.GetContextInfo()
		metadata = &mediaInfo{MIME: v.GetMimetype(), Filename: v.GetFileName(), Size: v.GetFileLength()}
	case m.GetAudioMessage() != nil:
		v := m.GetAudioMessage()
		out.Type = "audio"
		contextInfo = v.GetContextInfo()
		metadata = &mediaInfo{MIME: v.GetMimetype(), Size: v.GetFileLength(), Duration: v.GetSeconds()}
	case m.GetReactionMessage() != nil:
		out.Type = "reaction"
		out.ReplyTo = m.GetReactionMessage().GetKey().GetID()
	}
	if contextInfo != nil {
		out.ReplyTo = contextInfo.GetStanzaID()
	}
	if metadata != nil {
		data, _ := json.Marshal(metadata)
		out.Media = string(data)
	}
	// Ephemeral content expires locally. If the deadline is unavailable, skip it.
	duration := contextInfo.GetExpiration()
	if duration == 0 {
		duration = e.SourceWebMsg.GetEphemeralDuration()
	}
	if e.IsEphemeral || duration > 0 || e.SourceWebMsg.GetEphemeralExpirationTimestamp() > 0 {
		if out.Kind == "edit" && e.SourceWebMsg.GetEphemeralExpirationTimestamp() == 0 {
			return out, false
		}
		var expiry time.Time
		if absolute := e.SourceWebMsg.GetEphemeralExpirationTimestamp(); absolute > 0 {
			expiry = time.Unix(int64(absolute), 0)
		} else if duration > 0 && !e.Info.Timestamp.IsZero() {
			expiry = e.Info.Timestamp.Add(time.Duration(duration) * time.Second)
		} else {
			return out, false
		}
		if !expiry.After(now) {
			return out, false
		}
		out.ExpiresAt = &expiry
	}
	return out, !out.Timestamp.IsZero()
}
