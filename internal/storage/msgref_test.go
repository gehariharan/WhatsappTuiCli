package storage

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/gehariharan/wasync/internal/msgref"
)

const refAccount = "one@s.whatsapp.net"

func ref(t *testing.T, account, chat, message string) msgref.Ref {
	t.Helper()
	r, err := msgref.Decode(msgref.Encode(account, chat, message))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestMessageByRefMatchesLegacyRow(t *testing.T) {
	s, id := captureStore(t)
	ctx := context.Background()
	m := original("3EB0-ü🙂", "héllo 👋")
	mustApply(t, s, id, m)
	got, err := s.MessageByRef(ctx, id, refAccount, ref(t, refAccount, "one@g.us", m.MessageID))
	if err != nil {
		t.Fatal(err)
	}
	legacy := page(t, s, id, MessageQuery{Limit: 1}).Items[0]
	if !reflect.DeepEqual(got, legacy) {
		t.Fatalf("ref lookup %+v differs from legacy %+v", got, legacy)
	}
	if _, err = s.MessageByRef(ctx, id, refAccount, ref(t, refAccount, "one@g.us", "missing")); !errors.Is(err, ErrMessageNotFound) || !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing message: %v", err)
	}
}

func TestMessageByRefRejectsOtherAccounts(t *testing.T) {
	s, one := captureStore(t)
	ctx := context.Background()
	mustApply(t, s, one, original("shared-id", "account one"))
	two, err := s.UseAccount(ctx, "two@s.whatsapp.net")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveGroups(ctx, two, []Group{{ID: "one@g.us", Name: "Same chat"}}); err != nil {
		t.Fatal(err)
	}
	if err = s.SetEnabled(ctx, two, []string{"one@g.us"}, true); err != nil {
		t.Fatal(err)
	}
	mustApply(t, s, two, original("shared-id", "account two"))

	oneRef := ref(t, refAccount, "one@g.us", "shared-id")
	twoRef := ref(t, "two@s.whatsapp.net", "one@g.us", "shared-id")
	for name, call := range map[string]func() (Message, error){
		"one's ref under two":   func() (Message, error) { return s.MessageByRef(ctx, two, "two@s.whatsapp.net", oneRef) },
		"two's ref under one":   func() (Message, error) { return s.MessageByRef(ctx, one, refAccount, twoRef) },
		"mismatched id and jid": func() (Message, error) { return s.MessageByRef(ctx, two, refAccount, oneRef) },
		"empty account jid": func() (Message, error) {
			return s.MessageByRef(ctx, one, "", msgref.Ref{Chat: "one@g.us", Message: "shared-id"})
		},
	} {
		if m, err := call(); !errors.Is(err, ErrMessageNotFound) {
			t.Errorf("%s: got %+v, %v", name, m, err)
		}
	}
	for account, want := range map[int64]string{one: "account one", two: "account two"} {
		jid, r := refAccount, oneRef
		if account == two {
			jid, r = "two@s.whatsapp.net", twoRef
		}
		m, err := s.MessageByRef(ctx, account, jid, r)
		if err != nil || m.Text != want {
			t.Fatalf("account %d: %+v %v", account, m, err)
		}
	}
}

func TestMessageByRefSurvivesPurgeAndRecapture(t *testing.T) {
	s, id := captureStore(t)
	ctx := context.Background()
	mustApply(t, s, id, original("stable", "first capture"), original("filler", "other"))
	r := ref(t, refAccount, "one@g.us", "stable")
	before, err := s.MessageByRef(ctx, id, refAccount, r)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.PurgeGroup(ctx, id, "one@g.us"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.MessageByRef(ctx, id, refAccount, r); !errors.Is(err, ErrMessageNotFound) {
		t.Fatalf("purged message resolved: %v", err)
	}
	if err = s.SetEnabled(ctx, id, []string{"one@g.us"}, true); err != nil {
		t.Fatal(err)
	}
	recaptured := original("stable", "first capture")
	recaptured.Source = "history"
	// Another message arrives first, so the re-captured row gets a different local id.
	mustApply(t, s, id, original("newcomer", "after purge"), recaptured)
	after, err := s.MessageByRef(ctx, id, refAccount, r)
	if err != nil {
		t.Fatal(err)
	}
	if after.ID == before.ID {
		t.Fatalf("expected a new local row id, both %d", after.ID)
	}
	if after.MessageID != "stable" || after.Text != "first capture" {
		t.Fatalf("wrong message %+v", after)
	}
}

func TestMessageByRefHidesExpiredAndReturnsTombstones(t *testing.T) {
	s, id := captureStore(t)
	ctx := context.Background()
	future := time.Now().Add(time.Hour)
	expiring := original("expiring", "vanishing")
	expiring.ExpiresAt = &future
	mustApply(t, s, id, expiring, original("revoked", "secret"))
	r := ref(t, refAccount, "one@g.us", "expiring")
	if _, err := s.MessageByRef(ctx, id, refAccount, r); err != nil {
		t.Fatalf("unexpired message: %v", err)
	}
	// Move the expiry into the past without running Expire: the view alone must hide it.
	if _, err := s.db.ExecContext(ctx, "UPDATE messages SET expires_at=? WHERE message_id='expiring'", time.Now().Add(-time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if m, err := s.MessageByRef(ctx, id, refAccount, r); !errors.Is(err, ErrMessageNotFound) {
		t.Fatalf("expired message returned: %+v %v", m, err)
	}
	if err := s.Expire(ctx); err != nil {
		t.Fatal(err)
	}
	if m, err := s.MessageByRef(ctx, id, refAccount, r); !errors.Is(err, ErrMessageNotFound) {
		t.Fatalf("expired message returned after Expire: %+v %v", m, err)
	}

	mustApply(t, s, id, Message{GroupID: "one@g.us", MessageID: "revoked", Kind: "delete", MutationAt: time.Unix(1700000100, 0), Source: "live"})
	got, err := s.MessageByRef(ctx, id, refAccount, ref(t, refAccount, "one@g.us", "revoked"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "" || got.DeletedAt == nil {
		t.Fatalf("revoked message is not a tombstone: %+v", got)
	}
	var legacy Message
	for _, m := range page(t, s, id, MessageQuery{Limit: 50}).Items {
		if m.MessageID == "revoked" {
			legacy = m
		}
	}
	if !reflect.DeepEqual(got, legacy) {
		t.Fatalf("tombstone %+v differs from legacy %+v", got, legacy)
	}
}
