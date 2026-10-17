package whatsapp

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"whatsappsync/internal/localfs"
	"whatsappsync/internal/mediafiles"
	"whatsappsync/internal/storage"
)

type mediaJob struct {
	account    int64
	message    storage.Message
	attachment whatsmeow.DownloadableMessage
	extension  string
}

func (c *Client) ConfigureMedia(root string, enabled bool) {
	c.mediaRoot = root
	c.mediaEnabled = enabled
}
func (c *Client) startMediaWorker(ctx context.Context) {
	if c.mediaRoot == "" {
		return
	}
	ctx, c.mediaCancel = context.WithCancel(ctx)
	c.mediaQueue = make(chan mediaJob, 64)
	c.mediaDone = make(chan struct{})
	go func() {
		defer close(c.mediaDone)
		c.sweepMedia(ctx)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.sweepMedia(ctx)
			case job := <-c.mediaQueue:
				if ctx.Err() != nil {
					return
				}
				if err := c.downloadMedia(ctx, job); err != nil {
					if _, e := c.capture.store.SetMediaDownload(ctx, job.account, job.message.GroupID, job.message.MessageID, "failed", ""); e != nil && ctx.Err() == nil {
						c.StopCapture(e)
						return
					}
				}
			}
		}
	}()
}
func (c *Client) queueMedia(account int64, e *events.Message, m storage.Message) error {
	if !c.mediaEnabled || c.mediaQueue == nil || m.Kind != "" || m.ExpiresAt != nil || e.IsViewOnce || e.IsViewOnceV2 || e.IsViewOnceV2Extension {
		return nil
	}
	message := unwrap(e.Message)
	if message == nil {
		return nil
	}
	job := mediaJob{account: account, message: m, extension: ".bin"}
	var length uint64
	if image := message.GetImageMessage(); image != nil {
		job.attachment = proto.Clone(image).(whatsmeow.DownloadableMessage)
		length = image.GetFileLength()
		switch image.GetMimetype() {
		case "image/png":
			job.extension = ".png"
		case "image/jpeg":
			job.extension = ".jpg"
		case "image/webp":
			job.extension = ".webp"
		case "image/gif":
			job.extension = ".gif"
		}
	} else if document := message.GetDocumentMessage(); document != nil {
		job.attachment = proto.Clone(document).(whatsmeow.DownloadableMessage)
		length = document.GetFileLength()
		if document.GetMimetype() == "application/pdf" {
			job.extension = ".pdf"
		}
	} else {
		return nil
	}
	if length == 0 || length > MaxAttachmentSize || len(job.attachment.GetFileSHA256()) != 32 {
		_, err := c.capture.store.SetMediaDownload(c.capture.ctx, account, m.GroupID, m.MessageID, "skipped_size", "")
		return err
	}
	select {
	case c.mediaQueue <- job:
		return nil
	default:
		_, err := c.capture.store.SetMediaDownload(c.capture.ctx, account, m.GroupID, m.MessageID, "queue_full", "")
		return err
	}
}
func (c *Client) downloadMedia(ctx context.Context, job mediaJob) error {
	enabled, err := c.capture.store.MediaEligible(ctx, job.account, job.message.GroupID, job.message.MessageID)
	if err != nil || !enabled {
		return err
	}
	dir, err := mediafiles.Ensure(c.mediaRoot, job.account, job.message.GroupID)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(job.attachment.GetFileSHA256())
	name := fmt.Sprintf("%s-%x%s", mediafiles.Prefix(job.message.MessageID), hash, job.extension)
	path := filepath.Join(dir, name)
	if err = localfs.CheckPrivateFile(path); err == nil {
		_, err = c.capture.store.SetMediaDownload(ctx, job.account, job.message.GroupID, job.message.MessageID, "downloaded", path)
		return err
	} else if !os.IsNotExist(err) {
		return err
	}
	temp := path + ".part"
	f, err := localfs.OpenFile(temp)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	if err = f.Truncate(0); err != nil {
		f.Close()
		return err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	err = c.client.DownloadToFile(requestCtx, job.attachment, &boundedMediaFile{File: f})
	cancel()
	if err == nil {
		var info os.FileInfo
		info, err = f.Stat()
		if err == nil && info.Size() > MaxAttachmentSize {
			err = fmt.Errorf("media too large")
		}
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	// Recheck consent and deletion before making the file visible.
	valid, err := c.capture.store.MediaEligible(ctx, job.account, job.message.GroupID, job.message.MessageID)
	if err != nil || !valid {
		return err
	}
	if err = os.Rename(temp, path); err != nil {
		return err
	}
	valid, err = c.capture.store.SetMediaDownload(ctx, job.account, job.message.GroupID, job.message.MessageID, "downloaded", path)
	if err != nil || !valid {
		os.Remove(path)
	}
	return err
}

// Bound both encrypted download writes and in-place decryption writes.
type boundedMediaFile struct{ *os.File }

func (f *boundedMediaFile) Write(p []byte) (int, error) {
	offset, err := f.File.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	if offset > MaxAttachmentSize+64-int64(len(p)) {
		return 0, fmt.Errorf("media exceeds download limit")
	}
	return f.File.Write(p)
}
func (f *boundedMediaFile) WriteAt(p []byte, offset int64) (int, error) {
	if offset < 0 || offset > MaxAttachmentSize+64-int64(len(p)) {
		return 0, fmt.Errorf("media exceeds download limit")
	}
	return f.File.WriteAt(p, offset)
}
func (f *boundedMediaFile) Truncate(size int64) error {
	if size < 0 || size > MaxAttachmentSize+64 {
		return fmt.Errorf("media exceeds download limit")
	}
	return f.File.Truncate(size)
}

// io.Copy prefers ReaderFrom; do not inherit os.File.ReadFrom, which would
// bypass our Write limit when downloading an oversized response.
func (f *boundedMediaFile) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(struct{ io.Writer }{f}, r)
}

func (c *Client) removeMedia(account int64, chat, message string) {
	if err := mediafiles.RemoveMessage(c.mediaRoot, account, chat, message); err != nil {
		c.log.Warn("local media cleanup deferred; files may be open")
	}
}
func (c *Client) sweepMedia(ctx context.Context) {
	err := filepath.WalkDir(c.mediaRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if os.IsNotExist(walkErr) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !mediafiles.ManagedPath(c.mediaRoot, path) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".part") && time.Since(info.ModTime()) < 2*time.Minute {
			return nil
		}
		referenced, err := c.capture.store.MediaReferenced(ctx, path)
		if err != nil {
			return err
		}
		if !referenced {
			return os.Remove(path)
		}
		return nil
	})
	if err != nil && ctx.Err() == nil {
		c.log.Warn("local media cleanup incomplete; retrying later")
	}
}
