package whatsapp

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/gehariharan/wasync/internal/storage"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestLargeAttachmentOpensWithoutApplicationSizeCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large-document")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write([]byte("%PDF-1.7\n")); err != nil {
		t.Fatal(err)
	}
	// Sparse fixture: exercise a large regular file without allocating its bytes.
	const size = 256 << 20
	if err = file.Truncate(size); err != nil {
		t.Fatal(err)
	}
	file.Close()
	opened, mime, err := openAttachment(path)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if mime != "application/pdf" {
		t.Fatal(mime)
	}
	info, err := opened.Stat()
	if err != nil || info.Size() != size {
		t.Fatal(info, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := &contextReader{ctx: ctx, reader: io.NewSectionReader(opened, 0, size)}
	if _, err = reader.Read(make([]byte, 512)); !errors.Is(err, context.Canceled) {
		t.Fatal("encryption reads ignore cancellation", err)
	}
}

func TestLargeMediaQueuesUsingItsOwnDeclaredSize(t *testing.T) {
	const size = 1 << 30
	event := fixture(&waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{FileLength: proto.Uint64(size), FileSHA256: make([]byte, 32), Mimetype: proto.String("application/pdf")}})
	client := &Client{mediaEnabled: true, mediaQueue: make(chan mediaJob, 1)}
	if err := client.queueMedia(1, event, storage.Message{}); err != nil {
		t.Fatal(err)
	}
	select {
	case job := <-client.mediaQueue:
		if job.size != size {
			t.Fatal("declared size lost", job.size)
		}
	default:
		t.Fatal("large media rejected")
	}
}
