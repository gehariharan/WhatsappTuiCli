package whatsapp

import (
	"context"
	"github.com/gehariharan/wasync/internal/storage"
	"time"
)

// SyncInput bounds observation time; it is not a server completeness checkpoint.
type SyncInput struct {
	WaitSeconds int    `json:"wait_seconds"`
	History     bool   `json:"history"`
	Chat        string `json:"chat,omitempty"`
}
type SyncResult struct {
	Connection    string          `json:"connection"`
	Stored        int64           `json:"stored_during_wait"`
	Chats         []storage.Group `json:"chats"`
	Completeness  string          `json:"completeness"`
	ObservedUntil time.Time       `json:"observed_until"`
}
type Syncer func(context.Context, SyncInput) (SyncResult, error)
