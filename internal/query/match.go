package query

import (
	"fmt"
	"strings"

	"github.com/gehariharan/wasync/internal/storage"
)

// MatchChats resolves a chat by JID or name deterministically, shared by
// /api/v1/chats?match= and wasync query chats --v1 --match. Tiers, best first:
// exact JID; case-insensitive exact name; case-insensitive name prefix;
// case-insensitive name substring. All matches of the best non-empty tier are
// returned in input order; more than one means the name is ambiguous.
func MatchChats(groups []storage.Group, match string) []storage.Group {
	match = strings.TrimSpace(match)
	if match == "" {
		return nil
	}
	lower := strings.ToLower(match)
	tiers := []func(storage.Group) bool{
		func(g storage.Group) bool { return g.ID == match },
		func(g storage.Group) bool { return strings.EqualFold(strings.TrimSpace(g.Name), match) },
		func(g storage.Group) bool {
			return strings.HasPrefix(strings.ToLower(strings.TrimSpace(g.Name)), lower)
		},
		func(g storage.Group) bool { return strings.Contains(strings.ToLower(g.Name), lower) },
	}
	for _, tier := range tiers {
		var found []storage.Group
		for _, g := range groups {
			if tier(g) {
				found = append(found, g)
			}
		}
		if len(found) > 0 {
			return found
		}
	}
	return nil
}

// ChatCandidate identifies one chat in a chat_ambiguous error.
type ChatCandidate struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"chat_kind"`
}

// ChatAmbiguity is the details object of a chat_ambiguous error.
type ChatAmbiguity struct {
	Match      string          `json:"match"`
	Candidates []ChatCandidate `json:"candidates"`
}

const ChatAmbiguousHelp = "Several chats match; repeat the request with one candidate id (a chat JID) from details.candidates."

func (a ChatAmbiguity) Error() string {
	return fmt.Sprintf("chat name %q matches %d chats", a.Match, len(a.Candidates))
}

// V1Chats lists chats for v1: optionally only enabled ones, only those allow
// accepts (nil allows all, applied before matching so candidates never name a
// hidden chat), then narrowed by match. An ambiguous match returns a non-nil
// *ChatAmbiguity instead of a list.
func V1Chats(account int64, groups []storage.Group, enabled bool, match string, allow func(string) bool) (V1ChatList, *ChatAmbiguity) {
	visible := make([]storage.Group, 0, len(groups))
	for _, g := range groups {
		if (!enabled || g.Enabled) && (allow == nil || allow(g.ID)) {
			visible = append(visible, g)
		}
	}
	if match != "" {
		visible = MatchChats(visible, match)
		if len(visible) > 1 {
			a := &ChatAmbiguity{Match: strings.TrimSpace(match), Candidates: make([]ChatCandidate, 0, len(visible))}
			for _, g := range visible {
				a.Candidates = append(a.Candidates, ChatCandidate{g.ID, g.Name, storage.ChatKind(g.ID)})
			}
			return V1ChatList{}, a
		}
	}
	items := make([]Group, 0, len(visible))
	for _, g := range visible {
		items = append(items, FormatGroup(g))
	}
	return V1ChatList{account, items}, nil
}
