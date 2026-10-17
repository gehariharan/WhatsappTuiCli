package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"whatsappsync/internal/storage"
)

type CaptureStatus struct {
	Connection        string
	Error             string
	Stored, Skipped   int64
	AccountID         int64
	HistoryProcessing bool
	Observation       Observation
}
type capture struct {
	ctx             context.Context
	store           *storage.Store
	mu              sync.Mutex
	account, run    atomic.Int64
	stored, skipped atomic.Int64
	history         atomic.Bool
	failed          atomic.Bool
}

// EnableCapture must be called before connecting. Existing allowlists are ready
// before the server can deliver live or historical messages.
func (c *Client) EnableCapture(ctx context.Context, s *storage.Store) error {
	c.capture = &capture{ctx: ctx, store: s}
	if c.Paired() {
		if err := c.capture.initialize(c.AccountJID()); err != nil {
			return err
		}
	}
	c.startHistoryWorker(ctx)
	return nil
}
func (p *capture) initialize(jid string) error {
	if p.account.Load() != 0 {
		return nil
	}
	if jid == "" {
		return fmt.Errorf("no authenticated account identity")
	}
	id, err := p.store.UseAccount(p.ctx, jid)
	if err != nil {
		return err
	}
	run, err := p.store.StartCapture(p.ctx, id)
	if err != nil {
		return err
	}
	if err = p.store.Expire(p.ctx); err != nil {
		return err
	}
	p.account.Store(id)
	p.run.Store(run)
	return nil
}
func (c *Client) handleEvent(evt any) bool {
	c.rememberEventAnchors(evt)
	if c.capture != nil {
		if err := c.captureEvent(evt); err != nil {
			c.capture.failed.Store(true)
			c.connection.Store("capture stopped")
			c.lastError.Store("Archive operation failed. Capture stopped; check disk space and restart.")
			c.log.Error("capture stopped after archive failure")
			select {
			case c.fatal <- fmt.Errorf("archive operation failed; capture stopped: %w", err):
			default:
			}
			go c.client.Disconnect()
			return false
		}
	}
	c.onEvent(evt)
	return true
}
func (c *Client) captureEvent(evt any) error {
	p := c.capture
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failed.Load() {
		return fmt.Errorf("capture already stopped")
	}
	if p.account.Load() == 0 {
		if _, ok := evt.(*events.Connected); ok {
			if err := p.initialize(c.AccountJID()); err != nil {
				return err
			}
		} else {
			return nil
		}
	}
	account := p.account.Load()
	switch e := evt.(type) {
	case *events.Connected:
		return p.store.Resume(p.ctx, account)
	case *events.Disconnected:
		return p.store.Gap(p.ctx, account, "network disconnected; recovery unverified")
	case events.PermanentDisconnect:
		return p.store.Gap(p.ctx, account, "session rejected; recovery unverified")
	case *events.UndecryptableMessage:
		if e.Info.Chat.Server == types.GroupServer {
			enabled, err := p.store.IsEnabled(p.ctx, account, e.Info.Chat.String())
			if err != nil {
				return err
			}
			if enabled {
				return p.store.Gap(p.ctx, account, "a selected-group message could not be decrypted")
			}
		}
	case *events.Message:
		if e.Info.Chat.Server != types.GroupServer {
			return nil
		}
		enabled, err := p.store.IsEnabled(p.ctx, account, e.Info.Chat.String())
		if err != nil {
			return err
		}
		if !enabled {
			return nil
		}
		m, ok := Normalize(e, "live", time.Now())
		if !ok {
			p.skipped.Add(1)
			return nil
		}
		r, err := p.store.Apply(p.ctx, account, []storage.Message{m})
		p.stored.Add(int64(r.Stored))
		p.skipped.Add(int64(r.Skipped))
		return err
	case *events.HistorySync:
		if e.Data == nil {
			return nil
		}
		p.history.Store(true)
		defer p.history.Store(false)
		batch, err := p.store.BeginHistory(p.ctx, account, e.Data.GetSyncType().String(), e.Data.GetChunkOrder(), e.Data.GetProgress())
		if err != nil {
			return err
		}
		stored, skipped, failures := 0, 0, 0
		pending := make([]storage.Message, 0, 100)
		flush := func() error {
			if len(pending) == 0 {
				return nil
			}
			r, err := p.store.ApplyHistory(p.ctx, account, batch, pending)
			stored += r.Stored
			skipped += r.Skipped
			p.stored.Add(int64(r.Stored))
			p.skipped.Add(int64(r.Skipped))
			pending = pending[:0]
			return err
		}
		for _, conv := range e.Data.GetConversations() {
			jid, err := types.ParseJID(conv.GetID())
			if err != nil || jid.Server != types.GroupServer {
				continue
			}
			enabled, err := p.store.IsEnabled(p.ctx, account, jid.String())
			if err != nil {
				return err
			}
			if !enabled {
				skipped += len(conv.GetMessages())
				continue
			}
			for _, item := range conv.GetMessages() {
				web := item.GetMessage()
				if web == nil {
					failures++
					continue
				}
				event, err := c.client.ParseWebMessage(jid, web)
				if err != nil {
					failures++
					continue
				}
				m, ok := Normalize(event, "history", time.Now())
				if !ok {
					skipped++
					continue
				}
				pending = append(pending, m)
				if len(pending) == 100 {
					if err = flush(); err != nil {
						return err
					}
				}
			}
		}
		if err = flush(); err != nil {
			return err
		}
		if failures > 0 {
			c.lastError.Store(fmt.Sprintf("History batch had %d unparseable messages; coverage remains uncertain.", failures))
		}
		if err = p.store.EndHistory(p.ctx, batch, stored, skipped, failures); err != nil {
			return err
		}
		return c.finishHistoryPage(p.ctx, e)
	}
	return nil
}
func (c *Client) Status() CaptureStatus {
	status := CaptureStatus{Connection: "offline", Observation: c.Observation()}
	if v := c.connection.Load(); v != nil {
		status.Connection = v.(string)
	}
	if v := c.lastError.Load(); v != nil {
		status.Error = v.(string)
	}
	if p := c.capture; p != nil {
		status.Stored = p.stored.Load()
		status.Skipped = p.skipped.Load()
		status.AccountID = p.account.Load()
		status.HistoryProcessing = p.history.Load()
	}
	return status
}
func (c *Client) Maintain(ctx context.Context) error {
	p := c.capture
	if p == nil || p.account.Load() == 0 {
		return nil
	}
	if err := p.store.Expire(ctx); err != nil {
		return err
	}
	return p.store.Heartbeat(ctx, p.run.Load())
}
func (c *Client) finishCapture() error {
	p := c.capture
	if p == nil || p.run.Load() == 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var err error
	if !p.failed.Load() {
		err = p.store.EndCapture(ctx, p.run.Load())
	}
	return errors.Join(err, p.store.Expire(ctx))
}

func (c *Client) StopCapture(err error) {
	if c.capture != nil {
		c.capture.failed.Store(true)
	}
	c.connection.Store("capture stopped")
	c.lastError.Store("Archive operation failed. Capture stopped; check disk space and restart.")
	c.log.Error("capture stopped after archive failure")
	select {
	case c.fatal <- err:
	default:
	}
	go c.client.Disconnect()
}
