package transport

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gehariharan/whatsapp-axi/internal/contract"
	"github.com/gehariharan/whatsapp-axi/internal/testutil"
)

func acceptV1(v contract.VersionInfo) error {
	if v.APIVersion != 1 {
		return contract.Errorf(contract.CodeAXITooOld, "", "api %d", v.APIVersion)
	}
	return nil
}

func mustAPI(t *testing.T, addr string) *API {
	a, err := NewAPI(APIConfig{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestSelectPrefersAPI(t *testing.T) {
	srv := testutil.NewAPI(t)
	bin, log := testutil.FakeWasync(t, testutil.ModeOK)
	sel := Select(context.Background(), SelectOptions{API: mustAPI(t, srv.Addr()), Query: NewQuery(QueryConfig{Bin: bin}), Accept: acceptV1})
	if sel.Name() != NameAPI || sel.Version == nil || sel.APIErr != nil {
		t.Fatalf("selection %+v", sel)
	}
	_ = log // the query transport must not be run when the API answers
	if _, err := readIfExists(log); err == nil {
		t.Fatal("query transport ran although the API answered")
	}
}

func TestSelectFallsBackToQueryWhenOffline(t *testing.T) {
	bin, _ := testutil.FakeWasync(t, testutil.ModeOK)
	sel := Select(context.Background(), SelectOptions{API: mustAPI(t, testutil.ClosedAddr(t)), Query: NewQuery(QueryConfig{Bin: bin}), Accept: acceptV1})
	if sel.Name() != NameQuery || sel.Version == nil || codeOf(t, sel.APIErr) != contract.CodeOwnerOffline {
		t.Fatalf("selection %+v", sel)
	}
}

func TestSelectFallsBackWhenAPIIncompatible(t *testing.T) {
	srv := testutil.NewAPI(t)
	v := testutil.FakeVersion()
	v.APIVersion = 2
	srv.Version = v
	bin, _ := testutil.FakeWasync(t, testutil.ModeOK)
	sel := Select(context.Background(), SelectOptions{API: mustAPI(t, srv.Addr()), Query: NewQuery(QueryConfig{Bin: bin}), Accept: acceptV1})
	if sel.Name() != NameQuery || codeOf(t, sel.APIErr) != contract.CodeAXITooOld {
		t.Fatalf("selection %+v", sel)
	}
}

func TestSelectProbeTimeout(t *testing.T) {
	// A listener that accepts but never answers must not stall selection.
	srv := testutil.NewAPI(t)
	hang := make(chan struct{})
	t.Cleanup(func() { close(hang) })
	srv.Config.Handler = blockHandler(hang)
	bin, _ := testutil.FakeWasync(t, testutil.ModeOK)
	start := time.Now()
	sel := Select(context.Background(), SelectOptions{API: mustAPI(t, srv.Addr()), Query: NewQuery(QueryConfig{Bin: bin}), Accept: acceptV1, ProbeTimeout: 200 * time.Millisecond})
	if sel.Name() != NameQuery {
		t.Fatalf("selection %+v", sel)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("probe took %v", time.Since(start))
	}
}

func TestSelectNone(t *testing.T) {
	notFound := contract.Errorf(contract.CodeWasyncNotFound, "", "missing")
	sel := Select(context.Background(), SelectOptions{API: mustAPI(t, testutil.ClosedAddr(t)), QueryErr: notFound, Accept: acceptV1})
	if sel.Name() != "none" || !errors.Is(sel.QueryErr, notFound) || sel.Version != nil {
		t.Fatalf("selection %+v", sel)
	}
	bin, _ := testutil.FakeWasync(t, testutil.ModeAPI2)
	sel = Select(context.Background(), SelectOptions{API: mustAPI(t, testutil.ClosedAddr(t)), Query: NewQuery(QueryConfig{Bin: bin}), Accept: acceptV1})
	if sel.Name() != "none" || sel.Incompatible == nil || sel.Version == nil || sel.Version.APIVersion != 2 {
		t.Fatalf("selection %+v", sel)
	}
}
