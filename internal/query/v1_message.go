package query

import (
	"context"

	"whatsappsync/internal/storage"
)

func init() {
	for _, name := range []string{"chats", "chats.match", "messages", "search", "search.context", "message.ref", "thread"} {
		RegisterFeature(name)
	}
}

// Bounds shared by /api/v1 and wasync query --v1.
const (
	MaxSearchContext = 5
	MaxThreadWindow  = 20
	DefaultThread    = 5
)

// V1Message is the v1 message shape: every legacy Message field plus the
// stable reference and chat kind. Text is untrusted message content.
type V1Message struct {
	Message
	Ref      string `json:"ref"`
	ChatKind string `json:"chat_kind"`
}

func FormatV1Message(accountJID string, accountID int64, m storage.Message) V1Message {
	return V1Message{FormatMessage(accountID, m), RefFor(accountJID, m), storage.ChatKind(m.GroupID)}
}

// V1SearchHit is a search result with its surrounding messages from the same
// chat, oldest to newest; used only when context was requested.
type V1SearchHit struct {
	V1Message
	Before []V1Message `json:"before"`
	After  []V1Message `json:"after"`
}

// V1Thread is a message with the messages around it in its chat.
type V1Thread struct {
	Anchor V1Message   `json:"anchor"`
	Before []V1Message `json:"before"`
	After  []V1Message `json:"after"`
}

// V1MessageList is the data of message lists and searches.
type V1MessageList struct {
	AccountID int64 `json:"account_id"`
	Items     any   `json:"items"`
}

func formatV1Messages(accountJID string, accountID int64, ms []storage.Message) []V1Message {
	out := make([]V1Message, 0, len(ms))
	for _, m := range ms {
		out = append(out, FormatV1Message(accountJID, accountID, m))
	}
	return out
}

// V1Messages formats a page, keeping only messages in chats allow accepts
// (nil allows every chat). With around > 0 each item is a V1SearchHit.
func V1Messages(ctx context.Context, s *storage.Store, account *storage.Account, ms []storage.Message, around int, allow func(chat string) bool) (V1MessageList, error) {
	kept := make([]storage.Message, 0, len(ms))
	for _, m := range ms {
		if allow == nil || allow(m.GroupID) {
			kept = append(kept, m)
		}
	}
	if around <= 0 {
		return V1MessageList{account.ID, formatV1Messages(account.JID, account.ID, kept)}, nil
	}
	hits := make([]V1SearchHit, 0, len(kept))
	for _, m := range kept {
		before, after, err := s.MessageContext(ctx, account.ID, m.GroupID, m, around, around)
		if err != nil {
			return V1MessageList{}, err
		}
		hits = append(hits, V1SearchHit{FormatV1Message(account.JID, account.ID, m), formatV1Messages(account.JID, account.ID, before), formatV1Messages(account.JID, account.ID, after)})
	}
	return V1MessageList{account.ID, hits}, nil
}

// ThreadFor builds the thread around anchor.
func ThreadFor(ctx context.Context, s *storage.Store, account *storage.Account, anchor storage.Message, before, after int) (V1Thread, error) {
	older, newer, err := s.MessageContext(ctx, account.ID, anchor.GroupID, anchor, before, after)
	if err != nil {
		return V1Thread{}, err
	}
	return V1Thread{FormatV1Message(account.JID, account.ID, anchor), formatV1Messages(account.JID, account.ID, older), formatV1Messages(account.JID, account.ID, newer)}, nil
}

// V1ChatList is the data of chat lists.
type V1ChatList struct {
	AccountID int64   `json:"account_id"`
	Items     []Group `json:"items"`
}
