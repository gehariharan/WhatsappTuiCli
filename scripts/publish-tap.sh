#!/usr/bin/env bash
# Publish the wasync cask to the shared Homebrew tap.
#
# The tap may hold casks owned by other repositories (for example
# whatsapp-axi). This script only writes Casks/wasync.rb and the wasync
# download-strategy helper; it never deletes or edits other files under Casks/
# or Formula/. README.md and LICENSE are seeded only when the tap lacks them,
# after which the tap repository maintains them.
#
# Environment:
#   WASYNC_TAP_REMOTE  Tap remote (default git@github.com:gehariharan/homebrew-tap.git).
#                      A local path or file:// URL skips SSH key setup (tests).
#   WASYNC_TAP_BRANCH  Branch to publish to (default main).
set -euo pipefail
asset_directory="${1:?Usage: scripts/publish-tap.sh ASSET_DIRECTORY}"
asset_directory="$(cd "$asset_directory" && pwd)"
project_dir="$(cd "$(dirname "$0")/.." && pwd)"
tap_remote="${WASYNC_TAP_REMOTE:-git@github.com:gehariharan/homebrew-tap.git}"
tap_branch="${WASYNC_TAP_BRANCH:-main}"
max_attempts=3

case "$tap_remote" in
  /*|./*|../*|file://*)
    tap_ssh_dir="$(mktemp -d)" ;;
  *)
    source "$project_dir/scripts/tap-ssh.sh" ;;
esac
trap 'rm -rf "$tap_ssh_dir"' EXIT

cask_version() {
  # Prints the version "..." value of a cask read from stdin.
  sed -n 's/^[[:space:]]*version[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1
}

new_version="$(cask_version < "$asset_directory/wasync.rb")"
if [[ -z "$new_version" ]]; then
  echo "publish-tap: cannot read version from $asset_directory/wasync.rb" >&2
  exit 1
fi

# Stale-write protection. Versions are compared with GNU `sort -V` semantics:
# numeric runs compare as numbers, so 0.1.0-preview.20261019000000 sorts after
# 0.1.0-preview.20261018060349 and after 0.1.0-preview.5. Returns 0 when the
# tap already holds a strictly newer wasync version.
tap_has_newer() {
  local current
  current="$(git show "origin/$tap_branch:Casks/wasync.rb" 2>/dev/null | cask_version || true)"
  [[ -n "$current" && "$current" != "$new_version" ]] || return 1
  [[ "$(printf '%s\n%s\n' "$current" "$new_version" | sort -V | tail -n 1)" == "$current" ]] || return 1
  echo "publish-tap: tap already has wasync $current, newer than $new_version; not publishing." >&2
  return 0
}

# Writes wasync-owned files into the working tree and stages them. Returns 0
# when a commit was created, 1 when there is nothing to change.
apply_and_commit() {
  mkdir -p Casks lib
  cp "$asset_directory/wasync.rb" Casks/wasync.rb
  cp "$project_dir/packaging/homebrew/lib/wasync_github_release_download_strategy.rb" lib/
  local paths=(Casks/wasync.rb lib/wasync_github_release_download_strategy.rb)
  if [[ ! -e README.md ]]; then
    cp "$project_dir/packaging/homebrew/README.md" README.md
    paths+=(README.md)
  fi
  if [[ ! -e LICENSE ]]; then
    cp "$project_dir/LICENSE" LICENSE
    paths+=(LICENSE)
  fi
  git add -- "${paths[@]}"
  if git diff --cached --quiet; then
    return 1
  fi
  git commit -q -m "Update prebuilt wasync release $new_version"
}

git clone -q --branch "$tap_branch" "$tap_remote" "$tap_ssh_dir/tap"
cd "$tap_ssh_dir/tap"
git config user.name 'wasync release automation'
git config user.email '41898282+github-actions[bot]@users.noreply.github.com'

if tap_has_newer; then exit 0; fi
if ! apply_and_commit; then
  echo "publish-tap: tap already up to date for wasync $new_version."
  exit 0
fi

for (( attempt = 1; attempt <= max_attempts; attempt++ )); do
  if git push -q origin "HEAD:$tap_branch"; then
    echo "publish-tap: published wasync $new_version (attempt $attempt)."
    exit 0
  fi
  if (( attempt == max_attempts )); then break; fi
  echo "publish-tap: push rejected (attempt $attempt of $max_attempts); rebasing on $tap_branch." >&2
  if ! git pull -q --rebase origin "$tap_branch"; then
    # A concurrent wasync publish touched the same files: rebuild on top of it.
    git rebase --abort 2>/dev/null || true
    git fetch -q origin "$tap_branch"
    git reset -q --hard "origin/$tap_branch"
    if tap_has_newer; then exit 0; fi
    if ! apply_and_commit; then
      echo "publish-tap: tap already up to date for wasync $new_version."
      exit 0
    fi
    continue
  fi
  if tap_has_newer; then exit 0; fi
done
echo "publish-tap: failed to push to the tap after $max_attempts attempts." >&2
exit 1
