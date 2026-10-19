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
// to the caller: they never contain secrets or hashes. Help tells the agent
// which command a human must run; agents cannot run grant themselves.
type Error struct {
	Code       string        // unauthorized | forbidden | rate_limited
	Status     int           // 401 | 403 | 429
	Message    string        // short description
	Help       string        // human action, naming the missing scope and exact command
	Scope      string        // scope that was checked, if any
	Chat       string        // chat that was checked, if any
	RetryAfter time.Duration // rate_limited only
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

const grantHelpDefault = "Ask a human to create an agent credential in an interactive terminal: wasync agents grant NAME --scope read.chats,read.messages. Legacy api.token, send.token and approve.token are not accepted here."

func unauthorized(message string) *Error {
	return &Error{Code: CodeUnauthorized, Status: 401, Message: message, Help: grantHelpDefault}
}

// regrantCommand returns the exact commands to replace client c with one
// that also has extraScope and extraChat (either may be empty).
func regrantCommand(c *Client, extraScope, extraChat string, rate int) string {
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
