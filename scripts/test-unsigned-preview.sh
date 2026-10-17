#!/usr/bin/env bash
# A test-only exception for the one installed binary on a disposable hosted runner.
# This script is never part of the cask or installed release and never runs on user Macs.
set -euo pipefail
if [[ "${CI:-}" != true || "${RUNNER_ENVIRONMENT:-}" != github-hosted ]]; then
  echo "Unsigned preview execution exceptions are limited to GitHub-hosted CI runners." >&2
  exit 1
fi
binary="$(brew --prefix)/bin/wasync"
if /usr/bin/xattr -p com.apple.quarantine "$binary" >/dev/null 2>&1; then
  echo "CI ONLY: removing quarantine from the installed unsigned preview binary for functional tests."
  /usr/bin/xattr -d com.apple.quarantine "$binary"
fi
