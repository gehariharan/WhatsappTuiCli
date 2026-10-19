package msgref

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func raw(s string) string { return Prefix + base64.RawURLEncoding.EncodeToString([]byte(s)) }

func TestRoundTrip(t *testing.T) {
	for _, r := range []Ref{
		{"123@s.whatsapp.net", "456@g.us", "3EB0ABCDEF"},
		{"1:2@lid", "999@s.whatsapp.net", "x"},
		{"acct@lid", "grüße-👋@g.us", "ID-🙂-é"},
		{strings.Repeat("a", MaxPartLen), "c@g.us", strings.Repeat("m", 200)},
	} {
		ref := Encode(r.Account, r.Chat, r.Message)
		if !strings.HasPrefix(ref, Prefix) || strings.ContainsAny(ref, "=+/") {
			t.Fatalf("not url-safe unpadded: %q", ref)
		}
		got, err := Decode(ref)
		if err != nil {
			t.Fatalf("%+v: %v", r, err)
		}
		if got != r || got.String() != ref {
			t.Fatalf("round trip %+v -> %+v", r, got)
		}
	}
	// Stable: the same identifiers always yield the same reference.
	if Encode("a@lid", "b@g.us", "c") != Encode("a@lid", "b@g.us", "c") {
		t.Fatal("unstable")
	}
	if Encode("a@lid", "b@g.us", "c") == Encode("z@lid", "b@g.us", "c") {
		t.Fatal("reference is not account-scoped")
	}
}

func TestDecodeRejects(t *testing.T) {
	good := Encode("a@lid", "b@g.us", "c")
	cases := map[string]string{
		"empty":           "",
		"prefix only":     Prefix,
		"no prefix":       good[len(Prefix):],
		"wrong version":   "m2_" + good[len(Prefix):],
		"upper prefix":    "M1_" + good[len(Prefix):],
		"padding":         good + "=",
		"std alphabet":    Prefix + base64.RawStdEncoding.EncodeToString([]byte("a@lid\x00b@g.us\x00c??>>")),
		"newline":         good[:6] + "\n" + good[6:],
		"space":           good + " ",
		"two parts":       raw("a@lid\x00b@g.us"),
		"four parts":      raw("a@lid\x00b@g.us\x00c\x00d"),
		"empty account":   raw("\x00b@g.us\x00c"),
		"empty chat":      raw("a@lid\x00\x00c"),
		"empty message":   raw("a@lid\x00b@g.us\x00"),
		"chat no at":      raw("a@lid\x00bgus\x00c"),
		"chat leading at": raw("a@lid\x00@g.us\x00c"),
		"chat trailing":   raw("a@lid\x00b@\x00c"),
		"chat two at":     raw("a@lid\x00b@c@g.us\x00c"),
		"part too long":   raw("a@lid\x00b@g.us\x00" + strings.Repeat("m", MaxPartLen+1)),
		"ref too long":    Prefix + strings.Repeat("A", MaxRefLen),
		"non canonical":   good[:len(good)-1] + string(good[len(good)-1]+1),
	}
	for name, ref := range cases {
		if _, err := Decode(ref); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %q accepted or wrong error: %v", name, ref, err)
		}
	}
}

func TestNonCanonicalTrailingBits(t *testing.T) {
	// "a@lid\x00b@g.us\x00c" is 16 bytes, so the final base64 character
	// carries 4 unused bits; setting one must be rejected.
	good := Encode("a@lid", "b@g.us", "c")
	body := []byte(good[len(Prefix):])
	last := strings.IndexByte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_", body[len(body)-1])
	if last%4 != 0 {
		t.Skip("encoding has no spare bits")
	}
	body[len(body)-1] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"[last+1]
	if _, err := Decode(Prefix + string(body)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-canonical trailing bits accepted: %v", err)
	}
}

func FuzzDecode(f *testing.F) {
	f.Add(Encode("a@lid", "b@g.us", "c"))
	f.Add(Encode("123@s.whatsapp.net", "456-789@g.us", "3EB0C0FFEE"))
	f.Add(Encode("x@lid", "ü@g.us", "🙂"))
	f.Add(raw("a@lid\x00b@g.us"))
	f.Add(raw("a@lid\x00b@g.us\x00c\x00d"))
	f.Add(Prefix)
	f.Add("m1_====")
	f.Add("")
	f.Fuzz(func(t *testing.T, ref string) {
		r, err := Decode(ref)
		if err != nil {
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("untyped error %v", err)
			}
			return
		}
		if r.String() != ref || len(ref) > MaxRefLen {
			t.Fatalf("accepted non-canonical %q", ref)
		}
		for _, p := range []string{r.Account, r.Chat, r.Message} {
			if p == "" || len(p) > MaxPartLen || strings.ContainsRune(p, 0) {
				t.Fatalf("accepted bad part %q", p)
			}
		}
	})
}

func FuzzEncodeRoundTrip(f *testing.F) {
	f.Add("a@lid", "b@g.us", "c")
	f.Add("123@s.whatsapp.net", "ü@g.us", "🙂")
	f.Fuzz(func(t *testing.T, account, chat, message string) {
		ref := Encode(account, chat, message)
		r, err := Decode(ref)
		valid := account != "" && message != "" && jidLike(chat) &&
			!strings.ContainsRune(account+chat+message, 0) &&
			len(account) <= MaxPartLen && len(chat) <= MaxPartLen && len(message) <= MaxPartLen &&
			len(ref) <= MaxRefLen
		if valid != (err == nil) {
			t.Fatalf("valid=%v err=%v for %q %q %q", valid, err, account, chat, message)
		}
		if err == nil && (r != Ref{account, chat, message}) {
			t.Fatalf("round trip mismatch %+v", r)
		}
	})
}
