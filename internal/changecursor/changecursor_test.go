package changecursor

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

const epoch = "0123456789abcdef0123456789abcdef"

func raw(s string) string { return Prefix + base64.RawURLEncoding.EncodeToString([]byte(s)) }

func TestRoundTrip(t *testing.T) {
	for _, c := range []Cursor{{"owner@lid", epoch, 0}, {"123@s.whatsapp.net", epoch, 9223372036854775807}} {
		s := Encode(c)
		if !strings.HasPrefix(s, Prefix) || c.String() != s {
			t.Fatalf("encoding %q", s)
		}
		got, err := Decode(s)
		if err != nil || got != c {
			t.Fatalf("decode %q: %+v %v", s, got, err)
		}
	}
}

func TestRejectsNonCanonicalOrInvalid(t *testing.T) {
	valid := Encode(Cursor{"owner@lid", epoch, 7})
	for _, s := range []string{
		"", "c1_", "m1_" + valid[3:], "c2_" + valid[3:], valid + "=", valid + "A", " " + valid,
		strings.Replace(valid, "c1_", "C1_", 1),
		raw("owner@lid\x00" + epoch), raw("owner@lid\x00" + epoch + "\x007\x00x"),
		raw("\x00" + epoch + "\x007"), raw("owner\x00" + epoch + "\x007"), raw("own er@lid\x00" + epoch + "\x007"),
		raw("owner@lid\x00" + strings.ToUpper(epoch) + "\x007"), raw("owner@lid\x00abc\x007"),
		raw("owner@lid\x00" + epoch + "\x00-1"), raw("owner@lid\x00" + epoch + "\x0007"), raw("owner@lid\x00" + epoch + "\x00+7"),
		raw("owner@lid\x00" + epoch + "\x00"), raw("owner@lid\x00" + epoch + "\x0099999999999999999999"),
		Prefix + base64.URLEncoding.EncodeToString([]byte("owner@lid\x00"+epoch+"\x007")),
		Prefix + strings.Repeat("A", MaxLen),
		raw(strings.Repeat("a", MaxAccountLen) + "@lid\x00" + epoch + "\x007"),
	} {
		if _, err := Decode(s); !errors.Is(err, ErrInvalid) {
			t.Errorf("accepted %q: %v", s, err)
		}
	}
}
