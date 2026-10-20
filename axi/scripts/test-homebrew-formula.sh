#!/usr/bin/env bash
# Renders Formula/whatsapp-axi.rb from fixture values and checks its shape.
# Uses `ruby -c` when Ruby is installed, string checks always. Needs bash and
# python3; never contacts the network or the tap.
set -euo pipefail
project_dir="$(cd "$(dirname "$0")/.." && pwd)"
gen="$project_dir/scripts/homebrew-formula.py"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "ok - $*"; }

version=1.2.3
sum=1111111111111111111111111111111111111111111111111111111111111111
commit=0123456789ab
python3 "$gen" "$version" "$sum" "$work/whatsapp-axi.rb" --commit "$commit"
formula="$(cat "$work/whatsapp-axi.rb")"
bi=github.com/gehariharan/wasync/axi/internal/buildinfo

has() { [[ "$formula" == *"$1"* ]] || fail "formula lacks: $1"; }
has 'class WhatsappAxi < Formula'
has "url \"https://github.com/gehariharan/wasync/archive/refs/tags/v$version.tar.gz\""
has "sha256 \"$sum\""
has 'license "MIT"'
has 'depends_on "go" => :build'
has 'ENV["CGO_ENABLED"] = "0"'
has "-X $bi.Version=#{version}"
has "-X $bi.Commit=$commit"
has "-X $bi.Date=#{time.iso8601}"
has 'system "go", "build", *std_go_args(ldflags:), "./cmd/whatsapp-axi"'
has 'livecheck do'
has 'strategy :github_latest'
has 'depends_on "gehariharan/tap/wasync"'
has 'cd "axi" do'
has "version \"$version\""
has 'test do'
has 'shell_output("#{bin}/whatsapp-axi --version").strip'
[[ "$(tail -n 1 "$work/whatsapp-axi.rb")" == end ]] || fail "formula does not end with end"
pass "formula rendered with source url, sha256, go build dependency, ldflags, livecheck, caveats and test"

if command -v ruby >/dev/null 2>&1; then
  ruby -c "$work/whatsapp-axi.rb" >/dev/null || fail "ruby -c rejected the formula"
  pass "ruby -c accepts the formula"
else
  echo "skip - ruby not installed; string checks only"
fi

python3 "$gen" "$version" "$sum" "$work/no-commit.rb"
! grep -q 'buildinfo.Commit' "$work/no-commit.rb" || fail "Commit flag without --commit"
if command -v ruby >/dev/null 2>&1; then ruby -c "$work/no-commit.rb" >/dev/null || fail "ruby -c rejected the formula without --commit"; fi
pass "formula without --commit leaves Commit unset"

python3 "$gen" "$version" "$sum" "$work/again.rb" --commit "$commit"
cmp -s "$work/whatsapp-axi.rb" "$work/again.rb" || fail "rendering is not deterministic"
pass "rendering is deterministic"

# Bad input is refused and writes nothing.
expect_refusal() { # description, args...
  local what="$1"; shift
  rm -f "$work/bad.rb"
  if python3 "$gen" "$@" 2>/dev/null; then fail "accepted $what"; fi
  [[ ! -e "$work/bad.rb" ]] || fail "wrote a formula for $what"
  pass "refuses $what"
}
expect_refusal "a leading v" "v$version" "$sum" "$work/bad.rb"
expect_refusal "a short checksum" "$version" 1234 "$work/bad.rb"
expect_refusal "an uppercase checksum" "$version" "${sum//1/A}" "$work/bad.rb"
expect_refusal "a bad commit" "$version" "$sum" "$work/bad.rb" --commit 'main; rm'
echo "homebrew formula tests passed"
