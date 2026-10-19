package query

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"whatsappsync/internal/changecursor"
	"whatsappsync/internal/msgref"
	"whatsappsync/internal/storage"
)

// V1Change is one change-journal entry. It never carries message content;
// consumers fetch the message by Ref (read.messages) and delete their replica
// when the archive no longer returns it.
type V1Change struct {
	Sequence    int64     `json:"sequence"`
	Cursor      string    `json:"cursor"`
	Op          string    `json:"op"`
	OpVersion   int       `json:"op_version"`
	ChatID      string    `json:"chat_id"`
	MessageID   string    `json:"message_id"`
	Ref         string    `json:"ref,omitempty"`
	CommittedAt time.Time `json:"committed_at"`
}

// V1Changes is the data of GET /api/v1/changes and wasync query changes.
type V1Changes struct {
	Changes []V1Change `json:"changes"`
}

// V1Checkpoint is a consumer's committed position. Cursor and UpdatedAt are
// null when the consumer has never committed.
type V1Checkpoint struct {
	Consumer  string     `json:"consumer"`
	Cursor    *string    `json:"cursor"`
	UpdatedAt *time.Time `json:"updated_at"`
}

// ChangeLimit bounds a changes page.
const (
	DefaultChangeLimit = 100
	MaxChangeLimit     = 1000
)

// HelpResnapshot tells a consumer how to recover from cursor_expired.
const HelpResnapshot = "The cursor cannot be served by this archive (older than the 30-day journal retention, from a recreated or restored archive, or from another account). Resnapshot: re-read the chats and messages you replicate, then follow changes again starting without a cursor."

const helpInvalidCursor = "Pass a cursor returned by the changes feed (c1_...) unchanged, or omit it to start from the oldest retained change."

// CodedError carries a v1 error code and help text.
type CodedError struct {
	Code string
	Help string
	Err  error
}

func (e *CodedError) Error() string { return e.Err.Error() }
func (e *CodedError) Unwrap() error { return e.Err }

func coded(code, help string, err error) error { return &CodedError{Code: code, Help: help, Err: err} }

// ChangeCursor encodes the cursor of a journal position for account.
func ChangeCursor(accountJID, epoch string, sequence int64) string {
	return changecursor.Encode(changecursor.Cursor{Account: accountJID, Epoch: epoch, Sequence: sequence})
}

// FormatChange converts a journal entry; message ops get a message ref.
func FormatChange(accountJID, epoch string, c storage.Change) V1Change {
	out := V1Change{Sequence: c.Sequence, Cursor: ChangeCursor(accountJID, epoch, c.Sequence), Op: c.Op, OpVersion: c.OpVersion, ChatID: c.ChatID, MessageID: c.MessageID, CommittedAt: c.CommittedAt}
	if strings.HasPrefix(c.Op, "message.") && c.MessageID != "" {
		out.Ref = msgref.Encode(accountJID, c.ChatID, c.MessageID)
	}
	return out
}

// DecodeChangeCursor validates a cursor for the account and the archive's
// current epoch: malformed cursors are invalid_cursor; another account's or
// another epoch's cursor is cursor_expired.
func DecodeChangeCursor(value, accountJID, epoch string) (int64, error) {
	c, err := changecursor.Decode(value)
	if err != nil {
		return 0, coded(CodeInvalidCursor, helpInvalidCursor, err)
	}
	if c.Account != accountJID || c.Epoch != epoch {
		return 0, coded(CodeCursorExpired, HelpResnapshot, fmt.Errorf("change cursor belongs to another account or archive"))
	}
	return c.Sequence, nil
}

// ReadChanges returns one page of the account's journal after the cursor
// (empty: from the oldest retained change). allow, when not nil, hides
// changes of chats the caller may not see; the cursor still advances past them.
func ReadChanges(ctx context.Context, s *storage.Store, account *storage.Account, after string, limit int, allow func(chat string) bool) (V1Changes, *V1Page, error) {
	if limit < 1 || limit > MaxChangeLimit {
		return V1Changes{}, nil, coded(CodeInvalidArgument, "Use a limit from 1 to 1000.", fmt.Errorf("limit must be between 1 and %d", MaxChangeLimit))
	}
	position := int64(-1)
	if after != "" {
		info, err := s.Journal(ctx)
		if err != nil {
			return V1Changes{}, nil, err
		}
		if position, err = DecodeChangeCursor(after, account.JID, info.Epoch); err != nil {
			return V1Changes{}, nil, err
		}
	}
	page, err := s.Changes(ctx, account.ID, position, limit)
	if errors.Is(err, storage.ErrCursorExpired) {
		return V1Changes{}, nil, coded(CodeCursorExpired, HelpResnapshot, err)
	}
	if err != nil {
		return V1Changes{}, nil, err
	}
	data := V1Changes{Changes: make([]V1Change, 0, len(page.Changes))}
	for _, c := range page.Changes {
		if allow == nil || allow(c.ChatID) {
			data.Changes = append(data.Changes, FormatChange(account.JID, page.Epoch, c))
		}
	}
	return data, &V1Page{NextCursor: ChangeCursor(account.JID, page.Epoch, page.Next), HasMore: page.HasMore}, nil
}

// ValidConsumer enforces checkpoint consumer names of [a-z0-9-]{1,64}.
func ValidConsumer(name string) error {
	ok := len(name) >= 1 && len(name) <= 64
	for _, r := range name {
		ok = ok && (r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-')
	}
	if !ok {
		return coded(CodeInvalidArgument, "Use a consumer name of 1-64 characters a-z, 0-9 and -.", fmt.Errorf("invalid consumer name %q", name))
	}
	return nil
}

// ReadCheckpoint returns the consumer's checkpoint for the account.
func ReadCheckpoint(ctx context.Context, s *storage.Store, account *storage.Account, consumer string) (V1Checkpoint, error) {
	if err := ValidConsumer(consumer); err != nil {
		return V1Checkpoint{}, err
	}
	cp, found, err := s.Checkpoint(ctx, account.ID, consumer)
	if err != nil || !found {
		return V1Checkpoint{Consumer: consumer}, err
	}
	return FormatCheckpoint(account.JID, cp), nil
}

// FormatCheckpoint converts a stored checkpoint.
func FormatCheckpoint(accountJID string, cp storage.Checkpoint) V1Checkpoint {
	cursor := ChangeCursor(accountJID, cp.Epoch, cp.Sequence)
	updated := cp.UpdatedAt
	return V1Checkpoint{Consumer: cp.Consumer, Cursor: &cursor, UpdatedAt: &updated}
}
