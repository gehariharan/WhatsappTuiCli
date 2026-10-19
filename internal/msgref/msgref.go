// Package msgref encodes stable, account-scoped message references.
//
// A reference is "m1_" followed by the unpadded base64url encoding of
// account JID, chat JID and WhatsApp message ID joined by NUL bytes. It
// depends only on WhatsApp identifiers, never on local row IDs, so it stays
// valid across restarts, purge and re-capture and needs no migration.
// References are opaque to clients; only this package interprets them.
package msgref

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const (
	// Prefix identifies version 1 of the reference format.
	Prefix = "m1_"
	// MaxPartLen bounds each identifier in bytes.
	MaxPartLen = 256
	// MaxRefLen bounds the encoded reference in bytes.
	MaxRefLen = 1024
)

// ErrInvalid is matched (errors.Is) by every Decode failure.
var ErrInvalid = errors.New("invalid message ref")

// Ref identifies one message within one account's archive.
type Ref struct {
	Account string // account JID that captured the message
	Chat    string // chat JID (group or direct)
	Message string // WhatsApp message ID
}

var encoding = base64.RawURLEncoding

// Encode returns the canonical reference for the identifiers. It does not
// validate them; Decode rejects references built from out-of-range input.
func Encode(accountJID, chatJID, messageID string) string {
	return Prefix + encoding.EncodeToString([]byte(accountJID+"\x00"+chatJID+"\x00"+messageID))
}

// String returns the canonical encoding of r.
func (r Ref) String() string { return Encode(r.Account, r.Chat, r.Message) }

func invalid(reason string) error { return fmt.Errorf("%w: %s", ErrInvalid, reason) }

// Decode parses and strictly validates a reference. Only the exact canonical
// encoding of valid identifiers is accepted.
func Decode(ref string) (Ref, error) {
	if len(ref) > MaxRefLen {
		return Ref{}, invalid("too long")
	}
	if !strings.HasPrefix(ref, Prefix) {
		return Ref{}, invalid("unknown format")
	}
	body := ref[len(Prefix):]
	if body == "" {
		return Ref{}, invalid("empty")
	}
	raw, err := encoding.DecodeString(body)
	if err != nil {
		return Ref{}, invalid("malformed encoding")
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) != 3 {
		return Ref{}, invalid("wrong number of parts")
	}
	for _, p := range parts {
		if p == "" {
			return Ref{}, invalid("empty part")
		}
		if len(p) > MaxPartLen {
			return Ref{}, invalid("part too long")
		}
	}
	r := Ref{Account: parts[0], Chat: parts[1], Message: parts[2]}
	if !jidLike(r.Chat) {
		return Ref{}, invalid("chat is not a WhatsApp JID")
	}
	if r.String() != ref {
		return Ref{}, invalid("not canonical")
	}
	return r, nil
}

// jidLike reports whether s has the user@server shape of a WhatsApp JID.
func jidLike(s string) bool {
	at := strings.IndexByte(s, '@')
	return at > 0 && at < len(s)-1 && strings.IndexByte(s[at+1:], '@') < 0
}
