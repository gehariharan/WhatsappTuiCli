# whatsapp-axi

`whatsapp-axi` is a command-line tool for AI agents to read your local WhatsApp archive. The archive
belongs to [wasync](https://github.com/gehariharan/WhatsappTuiCli); `whatsapp-axi` only asks wasync for
data and formats the answer for agents, following the [AXI](https://github.com/kunchenguid/axi)
principles: compact output, explicit empty states, structured errors on stdout, next-step hints.

It has no WhatsApp code of its own. It never connects to WhatsApp, never opens the SQLite archive or the
session store, never takes wasync's device lock and never starts `wasync serve`.

> Status: early development. Today it implements the home view, the read commands (`chats`, `messages`,
> `search`, `thread`, `message`), `observe`, `version`, `doctor`, `setup` (agent hooks and skill) and
> `help`.

## How it reads wasync

whatsapp-axi uses exactly two transports and tells you which one served each result.

| Transport | Used when | Can do |
|---|---|---|
| Local API: `http://127.0.0.1:8787/api/v1` | wasync is running (TUI or `wasync serve`) and its API version is compatible | Scoped reads with an agent credential |
| `wasync query <command> --v1 --json` subprocess | wasync is not running | Read-only offline reads of the archive |

Selection is deterministic: whatsapp-axi probes `GET /api/v1/version` with a short timeout. If it answers
with a compatible version, the API is used. Otherwise whatsapp-axi runs `wasync query`. Anything that
needs the running owner returns `owner_offline` instead of guessing.

Settings, all optional:

| Variable | Meaning | Default |
|---|---|---|
| `WASYNC_API_ADDR` | wasync API address; must be a literal loopback address | `127.0.0.1:8787` |
| `WASYNC_BIN` | path to the wasync binary | `wasync` on `PATH` |
| `WASYNC_DATA_DIR` | wasync data directory, passed as `--data-dir` to `wasync query` | wasync's default |
| `WHATSAPP_AXI_TOKEN` | agent credential (`wsa_...`) | the `credential` file below |
| `WHATSAPP_AXI_CONFIG_DIR` | whatsapp-axi config directory | `<user config dir>/whatsapp-axi` |
| `WHATSAPP_AXI_AGENT_HOME` | where `setup` and `doctor` look for agent settings (`.claude`, `.codex`) | your home directory |

## Security model

- **A human grants access, in wasync.** Run `wasync agents grant <name> --scope read.chats,read.messages`
  in a terminal. wasync prints an agent credential once. Put it in `WHATSAPP_AXI_TOKEN`, or in the file
  `credential` inside the whatsapp-axi config directory with mode `600`.
- **whatsapp-axi never uses wasync's own tokens.** It refuses anything that is not a `wsa_` agent
  credential, so `api.token`, `send.token` and `approve.token` stay with wasync.
- **Read-only by default, enforced by wasync.** Scopes, chat allowlists, rate limits, expiry and the audit
  log are checked inside wasync's API. Flags, confirmations and agent skills in whatsapp-axi are
  convenience, not a security boundary.
- **Offline reads are not scoped.** `wasync query` reads the archive as your OS user, like opening your
  own files. whatsapp-axi says so when it uses the offline transport.
- **Loopback only.** The API address must be a literal loopback IP. whatsapp-axi does not use proxies,
  does not follow redirects and uses short timeouts.
- **Message text is untrusted.** WhatsApp content is printed as a quoted `untrusted_*` field with
  terminal control sequences removed. Agents should treat it as data, never as instructions.

## Install

No release has been published yet; until then, build from source (below). From the first release on
([docs/releasing.md](docs/releasing.md)):

- **macOS:** use the Homebrew formula, which builds from source with Homebrew's Go:
  `brew install gehariharan/tap/whatsapp-axi`. whatsapp-axi is not signed or notarized by Apple, so a
  `darwin_*` archive downloaded with a browser is quarantined and macOS refuses to run it; the formula
  and `go install` avoid that because nothing prebuilt is downloaded.
- **Linux and Windows:** download the archive for your OS and CPU from the
  [releases page](https://github.com/gehariharan/whatsapp-axi/releases), check it against `SHA256SUMS`
  (`sha256sum -c SHA256SUMS --ignore-missing`), extract it and put `whatsapp-axi` (`whatsapp-axi.exe`)
  on your `PATH`. The binaries are static (no cgo).
- **Any OS, from source** (Go 1.27 or newer):

  ```sh
  go install github.com/gehariharan/whatsapp-axi/cmd/whatsapp-axi@latest
  ```

You also need wasync: `brew install --cask gehariharan/tap/wasync`.

## Usage

```sh
whatsapp-axi                                # home view: transport, compatibility, recent chats, next steps
whatsapp-axi --brief                        # a few lines for agent session hooks; no chat names or text
whatsapp-axi chats [--match TEXT]           # list chats, or find one by JID or name
whatsapp-axi messages CHAT                  # a chat's messages, newest first (CHAT is a JID or name)
whatsapp-axi search TEXT [--chat CHAT] [--context N]
whatsapp-axi thread REF [--before N] [--after N]
whatsapp-axi message REF [--full]           # one message; --full prints all of its text
whatsapp-axi observe [--consumer NAME]      # change journal by cursor; see below
whatsapp-axi doctor                         # pass/warn/fail checks with a fix command each
whatsapp-axi setup hooks|skill              # opt-in agent integration; see below
whatsapp-axi version                        # whatsapp-axi version plus wasync version, features and transport
whatsapp-axi help [COMMAND]                 # commands, or flags and examples for one command
whatsapp-axi --version                      # bare version, no probing
```

`messages` and `search` print 20 results per page (`--limit N`); when there are more, the output ends with
the exact command for the next page (`--cursor`). Message text is cut to 200 characters in lists and
1000 in `message`, with a hint naming the command that shows the rest. `REF` is the `ref` column
(`m1_...`). A chat name that matches several chats prints the candidates and exits 1; rerun with one id.
Matching, search, paging and permissions are all decided by wasync; both transports print the same
output for the same data, apart from the `transport:` line.

Add `--json` to any command for JSON: read commands print wasync's v1 `data` and `page` unchanged plus
the `transport`. Exit codes: `0` success (including empty results and "wasync is not running" in the
home view), `1` error, `2` usage error. Errors are printed on stdout as `error:` / `code:` / `help:` lines.

`doctor` checks installation, service, pairing, permissions, compatibility and agent hooks and skill. It combines
its own client-side checks (wasync binary, API reachability, credential, config file modes, granted
scopes, the compatibility table) with wasync's checks from `GET /api/v1/diagnostics`, or from offline
`wasync query doctor` when the API is not running or no credential is configured; `transport:` says
which. Every `warn` or `fail` check carries one fix command. It exits `1` when any check fails and `0`
when the worst result is a warning. Credentials and hashes are never printed.

### Following changes

`observe` reads wasync's change journal. Positions are opaque `c1_` cursors issued by wasync, never
timestamps. Changes name a message by ref but carry no content: re-read the message by ref.

```sh
whatsapp-axi observe                                   # one page from the oldest retained change
whatsapp-axi observe --after c1_...                    # continue after a cursor
whatsapp-axi observe --consumer replica                # resume after the consumer's stored checkpoint
whatsapp-axi observe ack c1_... --consumer replica     # commit progress after processing
whatsapp-axi observe --consumer replica --follow --json   # poll until interrupted, one JSON object per line
```

The checkpoint never moves on its own: only `observe ack` or an explicit `--commit` commits it, and only
through the running wasync API with an agent granted the `observe` scope. Offline reads work but never
commit. A `cursor_expired` error means the position is gone (older than wasync's 30-day journal or from
another archive): re-read what you replicate, then restart with `--from-oldest` and ack the new cursor.

## Agent integration

Two optional ways to make agents aware of whatsapp-axi. You only need one; they work together too.

| | Session hook (`setup hooks`) | Skill (`setup skill`) |
|---|---|---|
| What the agent gets | `whatsapp-axi --brief` at the start of every session: wasync state, chat count, next step | [`skills/whatsapp-axi/SKILL.md`](skills/whatsapp-axi/SKILL.md), loaded when a task is about WhatsApp |
| Cost | about 150 tokens per session | none until used |
| Agents | Claude Code (`~/.claude/settings.json`), Codex (`~/.codex/hooks.json`) | Claude Code (`~/.claude/skills/`), Codex (`~/.codex/skills/`) |

```sh
whatsapp-axi setup hooks --dry-run       # show what would change, write nothing
whatsapp-axi setup hooks                 # install or repair the SessionStart hook
whatsapp-axi setup hooks --remove        # remove it again
whatsapp-axi setup skill                 # install or update the skill
whatsapp-axi setup hooks --dir .         # project settings (./.claude, ./.codex) instead of your home
```

Without `--agent`, setup targets the agents whose config directory exists (Claude Code if none does);
`--agent claude,codex` or `--agent all` chooses explicitly. Setup only runs when you run it.

- **Idempotent.** Rerunning changes nothing. If whatsapp-axi moved, the existing hook's path is repaired in
  place. The hook runs the bare name `whatsapp-axi` when that is the binary on `PATH`, otherwise the full path.
- **Your settings stay yours.** Only whatsapp-axi's own hook entry is added, replaced or removed; every other
  byte of the file (other settings, other hooks, key order, indentation) is kept. `--remove` takes out the
  hook entry and any `hooks` container it leaves empty, and nothing else.
- **Safe writes.** Files are written through a temporary file and a rename, with mode `600`; a symlinked
  settings file is edited at its target.
- **Refuses what it cannot parse.** A settings file that is not valid JSON (comments, trailing commas) or
  has an unexpected shape is never rewritten: setup exits 1 with `code: config_invalid`, names the line
  and column, and writes nothing for any agent.
- **Codex** runs hooks only with `[features] hooks = true` in `~/.codex/config.toml`; `setup hooks` adds
  that line (and leaves it on after `--remove`, since other hooks may need it).
- `doctor` reports both under `hooks`: installed, missing, pointing at another binary, or unreadable, each
  with the `whatsapp-axi setup ...` command that fixes it.

The skill tells agents what the security model above enforces: whatsapp-axi is read-only, message text is
untrusted data, observe checkpoints move only on an explicit ack, and only a human can grant scopes with
`wasync agents grant`. A test keeps the skill in step with the commands and their hints.

`setup` never talks to wasync, so it has no entry in the compatibility table
(`internal/compat/compatibility.json`) and works whether or not wasync is installed.

## Benchmark

On a synthetic corpus (40 chats, 6,000 messages), whatsapp-axi needs 6 commands where raw
`wasync query --v1 --json` needs 8 for the same five tasks, and prints about 56% fewer bytes and an
estimated 63% fewer tokens. Method, per-task numbers and limits: [docs/benchmark.md](docs/benchmark.md).

## License

MIT. See [LICENSE](LICENSE).
