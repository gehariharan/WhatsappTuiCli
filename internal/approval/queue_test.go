package approval

import (
	"testing"
	"time"
)

func TestApprovalRequiresExplicitTakeAndCannotReplay(t *testing.T) {
	q := &Queue{}
	r, err := q.Add(1, Input{Kind: "purge-chat", Chat: "one@g.us"})
	if err != nil {
		t.Fatal(err)
	}
	if q.Pending(1) == nil || q.Pending(2) != nil {
		t.Fatal("account isolation failed")
	}
	if _, err = q.Take(2, r.ID); err == nil {
		t.Fatal("other account approved request")
	}
	if _, err = q.Take(1, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = q.Take(1, r.ID); err == nil {
		t.Fatal("approval replay accepted")
	}
	q.Finish(r.ID, "completed", nil)
	if q.Get(1, r.ID).Status != "completed" {
		t.Fatal("outcome missing")
	}
}
func TestApprovalExpiryPreventsExecution(t *testing.T) {
	q := &Queue{}
	r, err := q.Add(1, Input{Kind: "clear-message", Chat: "one@g.us", Message: "one"})
	if err != nil {
		t.Fatal(err)
	}
	q.requests[0].Expires = time.Now().Add(-time.Second)
	if _, err = q.Take(1, r.ID); err == nil {
		t.Fatal("expired request executed")
	}
	if q.Get(1, r.ID).Status != "expired" {
		t.Fatal("expired state not reported")
	}
}
