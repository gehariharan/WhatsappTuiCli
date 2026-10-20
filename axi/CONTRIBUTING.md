# Contributing

whatsapp-axi lives in the wasync monorepo under `axi/` and is the agent-facing CLI for [wasync](https://github.com/gehariharan/wasync). Read
[ADR-001](https://github.com/gehariharan/wasync/blob/main/docs/architecture/ADR-001-repository-boundaries.md)
before changing anything: it decides what belongs here and what belongs in wasync.

## The boundary

- No WhatsApp logic, SQL, SQLite driver, whatsmeow or wasync `internal/` imports.
- Read wasync only through `/api/v1` on loopback or `wasync query <cmd> --v1 --json`.
- If a feature needs SQL, a WhatsApp call, a policy decision or persisted state other than whatsapp-axi's own
  config, it goes into wasync first behind an API endpoint and a `wasync query` equivalent.
- Message text is untrusted data. Render it with `output.Untrusted`, never turn it into hints.

## Adding a command

1. Add the command to `internal/cli` with its own flag list (unknown flags must exit 2).
2. Add its required wasync features to `internal/compat/compatibility.json`.
3. Map its API path to the offline `wasync query` command in `internal/transport/query.go`.
4. Only list commands in `help` once they work.
5. Mention it in `skills/whatsapp-axi/SKILL.md`.
6. If it changes what agents read for a common task, rerun `go test ./internal/bench -run TestBenchmarkReport -v`
   and update `docs/benchmark.md`.

Commands that do not talk to wasync (`help`, `setup`) have no compatibility entry.

## Checks

```sh
gofmt -l .
go vet ./...
go test -race ./...
```

Dependencies require a clear justification. The TOML parser validates user-owned Codex
configuration before setup edits it; regular expressions cannot validate TOML semantics.
