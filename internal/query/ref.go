package query

import (
	"github.com/gehariharan/wasync/internal/msgref"
	"github.com/gehariharan/wasync/internal/storage"
)

// RefFor returns the stable, account-scoped reference for an archived message.
// It depends only on WhatsApp identifiers, never on the local row id.
func RefFor(accountJID string, m storage.Message) string {
	return msgref.Encode(accountJID, m.GroupID, m.MessageID)
}

// MessageByRef is the response of `wasync query message --ref`: the legacy
// message shape under "item" plus the canonical reference.
type MessageByRef struct {
	AccountID int64   `json:"account_id"`
	Ref       string  `json:"ref"`
	Item      Message `json:"item"`
}
