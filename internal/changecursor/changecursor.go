// Package changecursor encodes opaque change-journal cursors.
//
// A cursor is "c1_" followed by the unpadded base64url encoding of the account
// JID, the archive's journal epoch and the decimal journal sequence joined by
// NUL bytes. Cursors are opaque to clients; only this package interprets
// them. A cursor is never a timestamp or a message row id.
package changecursor

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	// Prefix identifies version 1 of the cursor format.
	Prefix = "c1_"
	// MaxAccountLen bounds the account JID in bytes.
	MaxAccountLen = 256
	// MaxLen bounds the encoded cursor in bytes.
	MaxLen   = 512
	epochLen = 32
)

// ErrInvalid is matched (errors.Is) by every Decode failure.
var ErrInvalid = errors.New("invalid change cursor")

// Cursor is a position in one account's view of one archive's journal.
type Cursor struct {
	Account  string // account JID
	Epoch    string // journal epoch: 32 lowercase hex characters
	Sequence int64  // last consumed sequence (0 = before the first entry)
}

var encoding = base64.RawURLEncoding

// Encode returns the canonical cursor string. It does not validate c.
func Encode(c Cursor) string {
	return Prefix + encoding.EncodeToString([]byte(c.Account+"\x00"+c.Epoch+"\x00"+strconv.FormatInt(c.Sequence, 10)))
}

// String returns the canonical encoding of c.
func (c Cursor) String() string { return Encode(c) }

func invalid(reason string) error { return fmt.Errorf("%w: %s", ErrInvalid, reason) }

// Decode parses and strictly validates a cursor; only the exact canonical
// encoding of valid parts is accepted.
func Decode(s string) (Cursor, error) {
	if len(s) > MaxLen {
		return Cursor{}, invalid("too long")
	}
	body, ok := strings.CutPrefix(s, Prefix)
	if !ok {
		return Cursor{}, invalid("unknown format")
	}
	raw, err := encoding.Strict().DecodeString(body)
	if err != nil || body == "" {
		return Cursor{}, invalid("malformed encoding")
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) != 3 {
		return Cursor{}, invalid("wrong number of parts")
	}
	c := Cursor{Account: parts[0], Epoch: parts[1]}
	if c.Account == "" || len(c.Account) > MaxAccountLen || strings.Count(c.Account, "@") != 1 {
		return Cursor{}, invalid("account is not a WhatsApp JID")
	}
	for _, r := range c.Account {
		if r <= ' ' || r == 0x7f {
			return Cursor{}, invalid("account is not a WhatsApp JID")
		}
	}
	if len(c.Epoch) != epochLen {
		return Cursor{}, invalid("bad epoch")
	}
	for _, r := range c.Epoch {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return Cursor{}, invalid("bad epoch")
		}
	}
	c.Sequence, err = strconv.ParseInt(parts[2], 10, 64)
	if err != nil || c.Sequence < 0 || strconv.FormatInt(c.Sequence, 10) != parts[2] {
		return Cursor{}, invalid("bad sequence")
	}
	if Encode(c) != s {
		return Cursor{}, invalid("not canonical")
	}
	return c, nil
}
