package whatsapp

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
	"whatsappsync/internal/mediafiles"
	"whatsappsync/internal/storage"
)

func TestMediaDownloadBoundIncludesCopyFastPath(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "download"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	bounded := &boundedMediaFile{File: file}
	// TeeReader matches the upstream download path. io.Copy selects ReaderFrom.
	source := io.TeeReader(bytes.NewReader(make([]byte, MaxAttachmentSize+1024)), io.Discard)
	_, err = io.Copy(bounded, source)
	if err == nil {
		t.Fatal("oversized media bypassed the bounded writer")
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > MaxAttachmentSize+64 {
		t.Fatalf("wrote %d bytes beyond limit", info.Size())
	}
	if _, err = bounded.WriteAt([]byte("x"), int64(^uint64(0)>>1)); err == nil {
		t.Fatal("overflow offset accepted")
	}
}

func TestMediaSweepPreservesReferencedAndUnmanagedFiles(t *testing.T) {
	ctx := context.Background()
	dir := privateTestDir(t)
	store, err := storage.Open(ctx, filepath.Join(dir, "archive.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.UseAccount(ctx, "synthetic@lid")
	if err != nil {
		t.Fatal(err)
	}
	chat := "one@g.us"
	if err = store.SaveGroups(ctx, account, []storage.Group{{ID: chat, Name: "One"}}); err != nil {
		t.Fatal(err)
	}
	if err = store.SetEnabled(ctx, account, []string{chat}, true); err != nil {
		t.Fatal(err)
	}
	message := storage.Message{GroupID: chat, MessageID: "image", SenderID: "sender@lid", Type: "image", Kind: "normal", Timestamp: time.Now(), ReceivedAt: time.Now(), Media: `{"mime":"image/png"}`}
	if _, err = store.Apply(ctx, account, []storage.Message{message}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "media")
	destination, err := mediafiles.Ensure(root, account, chat)
	if err != nil {
		t.Fatal(err)
	}
	name := mediafiles.Prefix("image") + "-" + mediafiles.Prefix("content") + ".png"
	referenced := filepath.Join(destination, name)
	orphan := filepath.Join(destination, mediafiles.Prefix("orphan")+"-"+mediafiles.Prefix("content")+".png")
	unmanaged := filepath.Join(destination, "notes.txt")
	for _, path := range []string{referenced, orphan, unmanaged} {
		if err = os.WriteFile(path, []byte("synthetic"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := store.SetMediaDownload(ctx, account, chat, "image", "downloaded", referenced); err != nil || !ok {
		t.Fatal("could not reference media", err)
	}
	c := &Client{capture: &capture{ctx: ctx, store: store}, mediaRoot: root, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c.sweepMedia(ctx)
	if _, err = os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("orphan retained", err)
	}
	for _, path := range []string{referenced, unmanaged} {
		if _, err = os.Stat(path); err != nil {
			t.Fatal("referenced or unmanaged file removed", err)
		}
	}
	// Move the media root while metadata still contains the original absolute path.
	// This is also the failure mode of relocating the whole data directory.
	movedRoot := filepath.Join(dir, "relocated-media")
	if err = os.Rename(root, movedRoot); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(root, referenced)
	if err != nil {
		t.Fatal(err)
	}
	referenced = filepath.Join(movedRoot, relative)
	c.mediaRoot = movedRoot
	c.sweepMedia(ctx)
	if _, err = os.Stat(referenced); err != nil {
		t.Fatal("relocation deleted wanted media", err)
	}
	if err = store.ClearMessage(ctx, account, chat, "image"); err != nil {
		t.Fatal(err)
	}
	c.sweepMedia(ctx)
	if _, err = os.Stat(referenced); !os.IsNotExist(err) {
		t.Fatal("deleted message media retained", err)
	}
}
