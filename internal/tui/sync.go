package tui

import (
	"context"
	"fmt"
	"github.com/gehariharan/wasync/internal/storage"
	"github.com/gehariharan/wasync/internal/whatsapp"
	"time"
)

// Sync uses the existing connection. Incoming events persist independently.
func (s *Session) Sync(ctx context.Context, in whatsapp.SyncInput) (whatsapp.SyncResult, error) {
	result := whatsapp.SyncResult{Completeness: "unknown; reconnect and history delivery cannot verify missed-message recovery"}
	if in.WaitSeconds < 0 || in.WaitSeconds > 60 {
		return result, fmt.Errorf("wait must be between 0 and 60 seconds")
	}
	if s.client == nil {
		return result, fmt.Errorf("offline mode cannot sync")
	}
	if !s.network.TryLock() {
		return result, fmt.Errorf("connection operation is busy")
	}
	defer s.network.Unlock()
	before := s.client.Status()
	if before.Connection != "online" || before.AccountID == 0 {
		return result, fmt.Errorf("not connected; retry when the device is online")
	}
	groups, err := s.client.Groups(ctx)
	if err != nil {
		return result, err
	}
	if err = s.store.SaveGroups(ctx, before.AccountID, groups); err != nil {
		return result, err
	}
	chats, err := s.store.GroupSummaries(ctx, before.AccountID)
	if err != nil {
		return result, err
	}
	selected := make([]storage.Group, 0)
	for _, chat := range chats {
		if chat.Enabled && (in.Chat == "" || chat.ID == in.Chat) {
			selected = append(selected, chat)
			if in.History && chat.HistoryRequest != "sending" && chat.HistoryRequest != "waiting" {
				if err = s.store.QueueHistory(ctx, before.AccountID, chat.ID); err != nil {
					return result, fmt.Errorf("history request for %s: %w", chat.ID, err)
				}
			}
		}
	}
	if in.Chat != "" && len(selected) == 0 {
		return result, fmt.Errorf("chat must be discovered and enabled first")
	}
	timer := time.NewTimer(time.Duration(in.WaitSeconds) * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return result, ctx.Err()
	case <-timer.C:
	}
	after := s.client.Status()
	result.Connection = after.Connection
	result.Stored = after.Stored - before.Stored
	result.ObservedUntil = time.Now().UTC()
	result.Chats = selected[:0]
	chats, err = s.store.GroupSummaries(ctx, before.AccountID)
	if err != nil {
		return result, err
	}
	for _, chat := range chats {
		if chat.Enabled && (in.Chat == "" || in.Chat == chat.ID) {
			result.Chats = append(result.Chats, chat)
		}
	}
	if after.AccountID != before.AccountID || after.Connection != "online" || after.Error != "" {
		return result, fmt.Errorf("sync interrupted; inspect connection and capture status")
	}
	return result, nil
}
