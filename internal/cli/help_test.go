package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"whatsappsync/internal/whatsapp"
)

func TestCommandHelpIsDiscoverableWithoutLocalState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	var out, diagnostics bytes.Buffer
	app := &App{Out: &out, Err: &diagnostics, OpenClient: func(context.Context, string, *slog.Logger) (Client, error) {
		t.Fatal("help opened WhatsApp client")
		return nil, nil
	}}
	// Exercise the public JSON interface, then follow every advertised help path.
	if err := app.Run(context.Background(), []string{"--data-dir", dir, "help", "--json"}); err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		SchemaVersion int                                         `json:"schema_version"`
		RootHelp      string                                      `json:"root_help"`
		QueryHelp     string                                      `json:"query_help"`
		Commands      []struct{ Path, Usage, Description string } `json:"commands"`
	}
	if err := json.Unmarshal(out.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.SchemaVersion != 1 || len(catalog.Commands) == 0 {
		t.Fatal("missing versioned command catalog")
	}
	seen := make(map[string]bool)
	for _, name := range []string{"--data-dir", "--timeout", "--api-addr", "--no-api", "--download-media", "--pairing-full-history", "--version"} {
		if !strings.Contains(catalog.RootHelp, name) {
			t.Errorf("missing global flag %s", name)
		}
	}
	if !strings.Contains(catalog.QueryHelp, "next_cursor") || !strings.Contains(catalog.QueryHelp, "Exit codes") {
		t.Fatal("missing query conventions")
	}
	for _, command := range catalog.Commands {
		if seen[command.Path] || command.Usage == "" || command.Description == "" {
			t.Fatalf("invalid command: %+v", command)
		}
		seen[command.Path] = true
		for _, args := range [][]string{
			append([]string{"help"}, strings.Fields(command.Path)...),
			append(strings.Fields(command.Path), "--help"),
			append(strings.Fields(command.Path), "-h"),
		} {
			out.Reset()
			diagnostics.Reset()
			if err := app.Run(context.Background(), append([]string{"--data-dir", dir}, args...)); err != nil {
				t.Fatalf("%v: %v", args, err)
			}
			if !strings.Contains(out.String(), "Usage: wasync "+command.Usage) || diagnostics.Len() != 0 {
				t.Fatalf("%v: stdout=%q stderr=%q", args, out.String(), diagnostics.String())
			}
		}
	}
	// Independently require public command families so accidental catalog removal
	// cannot make discovery tests pass simply by testing fewer entries.
	for _, path := range []string{"tui", "run", "serve", "login", "logout", "groups list", "groups enable", "groups disable", "chats list", "chats enable", "chats disable", "sync", "media policy", "media download", "send", "clean", "delete", "actions status", "actions approve", "status", "query groups", "query chats", "query messages", "query search", "query status", "version", "help"} {
		if !seen[path] {
			t.Errorf("missing command %s", path)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("help created local state: %v", err)
	}
}

func TestHelpUnknownTopicsAndParsedFlags(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	var out, diagnostics bytes.Buffer
	app := &App{Out: &out, Err: &diagnostics}
	for _, args := range [][]string{{"help", "missing"}, {"media", "missing", "--help"}, {"help", "media", "policy", "unexpected"}} {
		if err := app.Run(context.Background(), append([]string{"--data-dir", dir}, args...)); err == nil {
			t.Fatalf("unknown topic accepted: %v", args)
		}
	}
	for _, args := range [][]string{
		{"media", "policy", "--chat", "123@g.us", "--help"},
		{"media", "download", "--chat", "123@g.us", "--help"},
		{"clean", "--chat", "123@g.us", "--help"},
		{"delete", "--scope", "everyone", "--help"},
		{"chats", "list", "--json", "--help"},
		{"query", "search", "meeting", "--help"},
		{"query", "messages", "--chat", "123@g.us", "--help"},
	} {
		out.Reset()
		diagnostics.Reset()
		if err := app.Run(context.Background(), append([]string{"--data-dir", dir}, args...)); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if out.Len() == 0 || diagnostics.Len() != 0 {
			t.Fatalf("help not on stdout: %v", args)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("help created local state: %v", err)
	}
}

func TestHelpAndFlagErrorsUseSeparateStreams(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	var out, diagnostics bytes.Buffer
	app := &App{Out: &out, Err: &diagnostics}
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"--api-addr", "0.0.0.0:8787", "media", "policy", "--help"}} {
		out.Reset()
		diagnostics.Reset()
		if err := app.Run(context.Background(), append([]string{"--data-dir", dir}, args...)); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if out.Len() == 0 || diagnostics.Len() != 0 {
			t.Fatalf("explicit help streams: %v stdout=%q stderr=%q", args, out.String(), diagnostics.String())
		}
	}
	for _, args := range [][]string{
		{"--bogus"}, {"send", "--bogus"}, {"clean", "--bogus"}, {"query", "messages", "--bogus"},
		{"media", "policy", "--message-id", "X"}, {"media", "download", "--mode", "auto"},
	} {
		out.Reset()
		diagnostics.Reset()
		if err := app.Run(context.Background(), append([]string{"--data-dir", dir}, args...)); err == nil {
			t.Fatalf("invalid invocation succeeded: %v", args)
		}
		if out.Len() != 0 || diagnostics.Len() == 0 {
			t.Fatalf("error streams: %v stdout=%q stderr=%q", args, out.String(), diagnostics.String())
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("help/errors created local state: %v", err)
	}
}

func TestHelpFlagAsValueIsNotHelp(t *testing.T) {
	var out bytes.Buffer
	app := &App{Out: &out}
	if handled, err := app.dispatchHelp([]string{"send", "--text", "--help"}); handled || err != nil {
		t.Fatal("literal message was interpreted as help")
	}
	client := &fakeSendingClient{}
	app.Err = &out
	app.OpenClient = func(context.Context, string, *slog.Logger) (Client, error) { return client, nil }
	if err := app.Run(context.Background(), []string{"--data-dir", filepath.Join(t.TempDir(), "data"), "send", "--to", "123@g.us", "--text", "--help"}); err != nil {
		t.Fatal(err)
	}
	if len(client.sent) != 1 || client.sent[0] != (whatsapp.SendInput{To: "123@g.us", Text: "--help"}) {
		t.Fatalf("literal text was not sent exactly once: %+v", client.sent)
	}
}
