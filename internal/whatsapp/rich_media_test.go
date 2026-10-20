package whatsapp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gehariharan/wasync/internal/mediafiles"
	"github.com/gehariharan/wasync/internal/storage"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
	"path/filepath"
)

func TestDesktopPairingIdentity(t *testing.T) {
	for platform, expected := range map[string]string{"darwin": "wasync (macOS)", "windows": "wasync (Windows)", "linux": "wasync (Linux)"} {
		if got := deviceName(platform); got != expected {
			t.Fatalf("%s: %s", platform, got)
		}
	}
	if store.DeviceProps.GetPlatformType() != waCompanionReg.DeviceProps_DESKTOP {
		t.Fatal("pairing is not desktop")
	}
}

func TestRichMediaQueueAndMetadata(t *testing.T) {
	hash := make([]byte, 32)
	cases := []struct {
		name, mime, extension string
		message               *waE2E.Message
	}{
		{"document", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", ".docx", &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{Mimetype: proto.String("application/vnd.openxmlformats-officedocument.wordprocessingml.document"), FileLength: proto.Uint64(100), FileSHA256: hash, FileName: proto.String("../../report.docx"), PageCount: proto.Uint32(2), MediaKey: []byte("private-key"), URL: proto.String("private-url")}}},
		{"video", "video/mp4", ".mp4", &waE2E.Message{VideoMessage: &waE2E.VideoMessage{Mimetype: proto.String("video/mp4"), FileLength: proto.Uint64(100), FileSHA256: hash, Seconds: proto.Uint32(12), MediaKey: []byte("private-key")}}},
		{"voice", "audio/ogg; codecs=opus", ".ogg", &waE2E.Message{AudioMessage: &waE2E.AudioMessage{Mimetype: proto.String("audio/ogg; codecs=opus"), FileLength: proto.Uint64(100), FileSHA256: hash, PTT: proto.Bool(true), Seconds: proto.Uint32(9), MediaKey: []byte("private-key")}}},
		{"sticker", "image/webp", ".webp", &waE2E.Message{StickerMessage: &waE2E.StickerMessage{Mimetype: proto.String("image/webp"), FileLength: proto.Uint64(100), FileSHA256: hash, IsAnimated: proto.Bool(true), MediaKey: []byte("private-key")}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := fixture(tc.message)
			m, ok := Normalize(event, "history", time.Now())
			if !ok || strings.Contains(m.Media, "private-") {
				t.Fatal("normalization failed or exposed keys", m)
			}
			var metadata map[string]any
			if err := json.Unmarshal([]byte(m.Media), &metadata); err != nil {
				t.Fatal(err)
			}
			if metadata["mime"] != tc.mime {
				t.Fatal(metadata)
			}
			if tc.name == "voice" && metadata["voice_note"] != true {
				t.Fatal("missing voice flag")
			}
			c := &Client{mediaEnabled: true, mediaQueue: make(chan mediaJob, 1)}
			if err := c.queueMedia(1, event, m); err != nil {
				t.Fatal(err)
			}
			select {
			case job := <-c.mediaQueue:
				if job.extension != tc.extension || len(job.attachment.GetFileSHA256()) != 32 {
					t.Fatal("wrong download job", job)
				}
				path := filepath.Join("media", "1", mediafiles.Prefix(m.GroupID), mediafiles.Prefix(m.MessageID)+"-"+mediafiles.Prefix("file")+job.extension)
				if !mediafiles.ManagedPath("media", path) {
					t.Fatal("download cannot be managed/cleaned", path)
				}
			default:
				t.Fatal("attachment not queued")
			}
			for _, excluded := range []string{"disabled", "view_once", "expiry", "mutation"} {
				c.mediaEnabled = true
				event.IsViewOnce = false
				m.Kind = ""
				m.ExpiresAt = nil
				switch excluded {
				case "disabled":
					c.mediaEnabled = false
				case "view_once":
					event.IsViewOnce = true
				case "expiry":
					expiry := time.Now().Add(time.Minute)
					m.ExpiresAt = &expiry
				case "mutation":
					m.Kind = "edit"
				}
				if err := c.queueMedia(1, event, m); err != nil {
					t.Fatal(err)
				}
				if len(c.mediaQueue) != 0 {
					t.Fatal("excluded media queued", excluded)
				}
			}
		})
	}
	lottie := fixture(&waE2E.Message{StickerMessage: &waE2E.StickerMessage{IsLottie: proto.Bool(true)}})
	c := &Client{mediaEnabled: true, mediaQueue: make(chan mediaJob, 1)}
	if err := c.queueMedia(1, lottie, storage.Message{}); err != nil || len(c.mediaQueue) != 0 {
		t.Fatal("Lottie queued", err)
	}
}

func TestReactionEventsIncludeRemovalAndRejectOtherChat(t *testing.T) {
	target := &waCommon.MessageKey{ID: proto.String("target"), RemoteJID: proto.String("123@g.us"), Participant: proto.String("456@lid")}
	for _, emoji := range []string{"👍", ""} {
		e := fixture(&waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Key: target, Text: proto.String(emoji), SenderTimestampMS: proto.Int64(1700000000000)}})
		m, ok := Normalize(e, "history", time.Now())
		var data struct {
			Removed bool   `json:"removed"`
			Emoji   string `json:"emoji"`
			Target  string `json:"target_message_id"`
		}
		if err := json.Unmarshal([]byte(m.Media), &data); err != nil {
			t.Fatal(err)
		}
		if !ok || m.Type != "reaction" || m.Text != emoji || m.ReplyTo != "target" || data.Removed != (emoji == "") || data.Emoji != emoji || data.Target != "target" {
			t.Fatal(m, data)
		}
	}
	target.RemoteJID = proto.String("other@g.us")
	if _, ok := Normalize(fixture(&waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Key: target, Text: proto.String("👍")}}), "live", time.Now()); ok {
		t.Fatal("cross-chat reaction accepted")
	}
}

func TestInlineViewOnceIsNeverArchivedOrQueued(t *testing.T) {
	for _, message := range []*waE2E.Message{
		{ImageMessage: &waE2E.ImageMessage{ViewOnce: proto.Bool(true)}},
		{VideoMessage: &waE2E.VideoMessage{ViewOnce: proto.Bool(true)}},
		{AudioMessage: &waE2E.AudioMessage{ViewOnce: proto.Bool(true)}},
		{ViewOnceMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}}},
	} {
		event := fixture(message)
		if _, ok := Normalize(event, "history", time.Now()); ok {
			t.Fatal("view-once content archived")
		}
		// No capture store: trying to queue an invalid-size attachment would fail;
		// exclusion must happen before touching either storage or the queue.
		c := &Client{mediaEnabled: true, mediaQueue: make(chan mediaJob, 1)}
		if err := c.queueMedia(1, event, storage.Message{}); err != nil || len(c.mediaQueue) != 0 {
			t.Fatal("view-once content queued", err)
		}
	}
}

func TestReactionDirectChatUsesOnlySuppliedAlternate(t *testing.T) {
	e := fixture(&waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Key: &waCommon.MessageKey{ID: proto.String("target"), RemoteJID: proto.String("123@lid")}, Text: proto.String("👍")}})
	e.Info.Chat = types.NewJID("15551234567", types.DefaultUserServer)
	e.Info.IsGroup = false
	e.Info.SenderAlt = types.NewJID("123", types.HiddenUserServer)
	if _, ok := Normalize(e, "live", time.Now()); !ok {
		t.Fatal("supplied alternate rejected")
	}
	e.Info.SenderAlt = types.EmptyJID
	if _, ok := Normalize(e, "live", time.Now()); ok {
		t.Fatal("unrelated direct target accepted")
	}
	e.Info.IsFromMe = true
	e.Info.RecipientAlt = types.NewJID("123", types.HiddenUserServer)
	if _, ok := Normalize(e, "live", time.Now()); !ok {
		t.Fatal("outgoing supplied alternate rejected")
	}
}
