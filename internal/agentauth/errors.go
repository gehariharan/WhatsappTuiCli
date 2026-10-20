package agentauth

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Error codes returned to agents. Map Status to the HTTP status code.
const (
	CodeUnauthorized = "unauthorized"
	CodeForbidden    = "forbidden"
	CodeRateLimited  = "rate_limited"
)

// Error is a typed authorization failure. Message and Help are safe to return
// to the caller: they never contain secrets or hashes. Help describes the human
// action required; agents cannot grant themselves access.
type Error struct {
	Code       string        // unauthorized | forbidden | rate_limited
	Status     int           // 401 | 403 | 429
	Message    string        // short description
	Help       string        // human repair guidance
	Scope      string        // scope that was checked, if any
	Chat       string        // chat that was checked, if any
	RetryAfter time.Duration // rate_limited only
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

const grantHelpDefault = "Ask a human to create an agent credential in an interactive terminal: wasync agents grant NAME --scope read.chats,read.messages. Legacy api.token, send.token and approve.token are not accepted here."

func unauthorized(message string) *Error {
	return &Error{Code: CodeUnauthorized, Status: 401, Message: message, Help: grantHelpDefault}
}

// RegrantCommand documents the replacement policy shared by these error hints.
func regrantCommand(c *Client, extraScope, extraChat string, rate int) string {
	if c.ExpiresAt != nil {
		return fmt.Sprintf("Ask a human to review wasync agents list and explicitly choose an expiry when replacing expiring agent %s (current expiry %s); retain its chat allowlist and rate limit. Do not create a permanent replacement.", c.Name, c.ExpiresAt.UTC().Format(time.RFC3339))
	}
	set := map[string]bool{}
	for _, s := range c.Scopes {
		set[s] = true
	}
	if extraScope != "" {
		set[extraScope] = true
	}
	chats := append([]string(nil), c.Chats...)
	if extraChat != "" {
		found := false
		for _, existing := range chats {
			found = found || existing == extraChat
		}
		if !found {
			chats = append(chats, extraChat)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "wasync agents revoke %s && wasync agents grant %s --scope %s", c.Name, c.Name, strings.Join(canonicalScopes(set), ","))
	needChat := false
	for s := range set {
		needChat = needChat || RequiresAllowlist(s)
	}
	if len(chats) == 0 && needChat {
		b.WriteString(" --chat CHAT_JID")
	}
	for _, chat := range chats {
		b.WriteString(" --chat " + chat)
	}
	if rate <= 0 {
		rate = c.RatePerMinute
	}
	if rate != DefaultRatePerMinute {
		b.WriteString(" --rate " + strconv.Itoa(rate))
	}
	return b.String()
}
