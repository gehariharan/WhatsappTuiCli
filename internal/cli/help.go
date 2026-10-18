package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// One catalog backs both command help and agent discovery. Usage includes every
// accepted command-specific flag; global flags are described by the root help.
type commandHelp struct {
	Path        string `json:"path"`
	Usage       string `json:"usage"`
	Description string `json:"description"`
	Details     string `json:"details,omitempty"`
}

var commandCatalog = []commandHelp{
	{"tui", "tui [--offline]", "Open the interactive TUI (default when no command is supplied).", "Capture enabled chats while connected; --offline browses the local archive. An interactive terminal is required."},
	{"run", "run [--offline]", "Alias for tui.", "Capture continues until exit; --offline browses without connecting."},
	{"serve", "serve [--offline]", "Run headless capture and the loopback API.", "--offline serves the local archive without connecting. Cannot be combined with --no-api. Keep this or the TUI running for owner-channel commands."},
	{"login", "login [--select=true|false] [--observe DURATION]", "Pair by QR or reuse the saved session, then discover groups.", "--select defaults to terminal interactivity. --observe defaults to 0 and counts events without saving message bodies. Use tui or serve for capture."},
	{"logout", "logout", "Unlink the saved WhatsApp device session.", "Connects to WhatsApp when a saved session exists. Local archives and chat preferences remain on disk."},
	{"groups", "groups list|enable|disable", "Discover and select group chats for capture.", "Use help groups list, help groups enable, or help groups disable."},
	{"groups list", "groups list [--refresh] [--json]", "List cached groups and capture preferences.", "--refresh connects using the saved session to refresh metadata; otherwise reads local state."},
	{"groups enable", "groups enable GROUP_ID|--all", "Persist capture preference for one group or all cached groups.", "Enabling queues best-effort history when capture runs; it cannot guarantee a complete archive. Example: wasync groups enable '123456789@g.us'"},
	{"groups disable", "groups disable GROUP_ID|--all", "Disable capture for one group or all cached groups.", "Previously archived messages remain local."},
	{"chats", "chats list|enable|disable", "Manage capture for groups and direct chats.", "Direct-chat discovery is best effort, not a complete WhatsApp inbox listing."},
	{"chats list", "chats list [--refresh] [--json]", "List cached groups and direct chats.", "--refresh connects to refresh group metadata; direct chats are discovered from delivered events."},
	{"chats enable", "chats enable CHAT_ID|--all", "Persist capture preference for one chat or all cached chats.", "An explicit direct-chat ID can be added. Enabling does not start a background process: keep tui or serve running."},
	{"chats disable", "chats disable CHAT_ID|--all", "Disable capture for one chat or all cached chats.", "Previously archived messages remain local."},
	{"sync", "sync [--wait DURATION] [--history] [--chat CHAT_ID]", "Capture now, reusing a running owner when available.", "--wait defaults to 30s, allowed 0 to 60s. --history requests best-effort history for enabled chats; --chat restricts it. History needs a known message anchor and phone availability; a successful request does not imply full recovery."},
	{"media", "media policy|download", "Configure automatic media downloads or download one attachment.", "Use help media policy or help media download. No fixed application media-size cap; WhatsApp transfer limits and remote availability apply."},
	{"media policy", "media policy --chat CHAT_ID [--mode auto|manual|inherit]", "Read or persist a per-chat media policy; returns JSON.", "auto downloads media on future deliveries, including delivered history; manual requires an explicit download; inherit follows --download-media / WASYNC_DOWNLOAD_MEDIA. Omit --mode to read the saved policy. This setting persists across restarts and does not bulk-download older archived messages or enable capture."},
	{"media download", "media download --chat CHAT_ID --message-id MESSAGE_ID", "Queue a one-time attachment download; returns JSON.", "Requires a running connected tui or serve owner with its command channel, enabled capture, and retained download metadata. Does not change the saved policy. Older archives may lack the metadata, and WhatsApp may no longer serve the attachment."},
	{"send", "send --to CHAT_ID [--text TEXT] [--file PATH] [--type image|document]", "Explicitly send a text message or local attachment.", "At least --text or --file is required. With a file, --text is the caption and --type defaults to document; image supports JPEG/PNG. Reuses a running owner or connects with the saved session. A server acknowledgement is not proof of recipient delivery; do not automatically retry uncertain sends."},
	{"clean", "clean --chat CHAT_ID (--message-id MESSAGE_ID|--all)", "Request deletion from the local archive only.", "Requires a running owner. Returns an approval request without deleting anything. A human must approve through the TUI or interactive actions approve; there is no --yes."},
	{"delete", "delete --chat CHAT_ID --scope everyone --message-id MESSAGE_ID\n       wasync delete --chat CHAT_ID --scope my-whatsapp-chat", "Request a WhatsApp deletion with human approval.", "everyone requests revocation of your own message, subject to WhatsApp limits. my-whatsapp-chat deletes your WhatsApp chat state and leaves the local archive. Requires a running owner and subsequent human approval; there is no --yes."},
	{"actions", "actions status|approve REQUEST_ID", "Inspect or interactively approve a destructive request.", "Requires the running owner's command channel. REQUEST_ID is the 32-character hexadecimal ID returned by clean or delete."},
	{"actions status", "actions status REQUEST_ID", "Read the status of a pending or completed request as JSON.", "Does not approve or execute the request."},
	{"actions approve", "actions approve REQUEST_ID", "Ask a human to approve a pending destructive request.", "Requires an interactive terminal and a typed confirmation. Agents cannot bypass confirmation with --yes."},
	{"status", "status", "Inspect saved local pairing state and cached groups.", "Does not check network connectivity. For read-only agent queries while capture runs, prefer query status."},
	{"query", "query groups|chats|messages|search|status", "Read-only JSON queries for agents, without an API or WhatsApp connection.", "Use help query messages or help query search for filters and pagination. All results are JSON; exit codes: 0 success (including empty items), 1 error, 130 cancellation. Queries never download media."},
	{"query groups", "query groups [--enabled] [--json]", "Read cached group summaries as JSON.", "--enabled restricts results to enabled groups. No WhatsApp connection or running owner required."},
	{"query chats", "query chats [--enabled] [--json]", "Read cached group and direct-chat summaries as JSON.", "--enabled restricts results to enabled chats. No WhatsApp connection or running owner required."},
	{"query messages", "query messages --chat CHAT_ID [--since RFC3339] [--until RFC3339] [--sender SENDER_ID] [--limit N] [--cursor CURSOR] [--json]", "Read archived messages as paginated JSON, newest first.", "--group is a legacy alias for --chat; do not combine them. --since is inclusive, --until exclusive. --limit is 1 to 200 (default 50). Reuse next_cursor with identical filters; empty items is success. Never downloads attachments."},
	{"query search", "query search PHRASE [--chat CHAT_ID] [--since RFC3339] [--until RFC3339] [--sender SENDER_ID] [--limit N] [--cursor CURSOR] [--json]", "Search archived text and captions using SQLite FTS; returns JSON.", "Quote PHRASE as one argument (maximum 1024 bytes). --group is a legacy alias for --chat. Dates are inclusive --since and exclusive --until; --limit is 1 to 200 (default 50). Pass next_cursor with identical filters. Does not index document bodies or download media."},
	{"query status", "query status [--json]", "Read persisted sync observations as JSON.", "Current network connectivity is unknown; reconnects do not prove missed messages were recovered."},
	{"version", "version", "Show installed build information.", "Also available as wasync --version."},
	{"help", "help [COMMAND [SUBCOMMAND]]\n       wasync help --json", "Discover all commands without connecting or opening the archive.", "Use wasync COMMAND [SUBCOMMAND] --help as an alternative. --json emits a versioned command catalog, root help, and query conventions. Global flags precede the command."},
}

func (a *App) printHelp(path string) error {
	return a.writeHelp(a.Out, path)
}

func (a *App) writeHelp(out io.Writer, path string) error {
	if path == "" {
		_, err := fmt.Fprint(out, usage)
		return err
	}
	for _, command := range commandCatalog {
		if command.Path == path {
			_, err := fmt.Fprintf(out, "Usage: wasync %s\n\n%s\n\n%s\n\nGlobal flags precede the command. See wasync help for global flags.\n", command.Usage, command.Description, command.Details)
			return err
		}
	}
	return fmt.Errorf("unknown help topic %q; run wasync help --json to list commands", path)
}

// Recognize explicit help paths before configuration validation or local I/O.
// Only interpret a trailing help flag when all preceding arguments are command
// path words; a value such as `send --text --help` must remain a literal value.
func (a *App) dispatchHelp(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if args[0] == "help" {
		if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
			return true, a.printHelp("help")
		}
		if len(args) == 2 && args[1] == "--json" {
			return true, json.NewEncoder(a.Out).Encode(struct {
				SchemaVersion int           `json:"schema_version"`
				RootHelp      string        `json:"root_help"`
				QueryHelp     string        `json:"query_help"`
				Commands      []commandHelp `json:"commands"`
			}{1, usage, queryUsage, commandCatalog})
		}
		return true, a.printHelp(strings.Join(args[1:], " "))
	}
	last := args[len(args)-1]
	if last != "--help" && last != "-h" {
		return false, nil
	}
	for _, arg := range args[:len(args)-1] {
		if strings.HasPrefix(arg, "-") {
			return false, nil
		}
	}
	path := strings.Join(args[:len(args)-1], " ")
	for _, command := range commandCatalog {
		if command.Path == path {
			return true, a.printHelp(path)
		}
	}
	// A positional argument may precede --help (notably query search PHRASE).
	// Let the command's parser distinguish its arguments from command path words.
	return false, nil
}

// The flag package emits usage both for explicit help and for parse failures.
// Buffer it until the result is known so successful help goes to stdout and
// invalid invocations leave stdout clean for machine-readable command results.
func (a *App) parseFlags(f *flag.FlagSet, args []string) error {
	var output bytes.Buffer
	original := f.Output()
	f.SetOutput(&output)
	defer f.SetOutput(original)
	err := f.Parse(args)
	destination := a.Err
	if errors.Is(err, flag.ErrHelp) {
		destination = a.Out
	}
	_, writeErr := io.Copy(destination, &output)
	if writeErr != nil {
		return writeErr
	}
	return err
}
