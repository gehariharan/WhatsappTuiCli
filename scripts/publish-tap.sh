#!/usr/bin/env bash
set -euo pipefail
asset_directory="${1:?Usage: scripts/publish-tap.sh ASSET_DIRECTORY}"
project_dir="$(cd "$(dirname "$0")/.." && pwd)"
source "$project_dir/scripts/tap-ssh.sh"
trap 'rm -rf "$tap_ssh_dir"' EXIT
git clone --depth 1 git@github.com:gehariharan/homebrew-tap.git "$tap_ssh_dir/tap"
mkdir -p "$tap_ssh_dir/tap/Casks" "$tap_ssh_dir/tap/lib"
cp "$asset_directory/wasync.rb" "$tap_ssh_dir/tap/Casks/wasync.rb"
cp "$project_dir/packaging/homebrew/lib/wasync_github_release_download_strategy.rb" "$tap_ssh_dir/tap/lib/"
cp "$project_dir/packaging/homebrew/README.md" "$tap_ssh_dir/tap/README.md"
cd "$tap_ssh_dir/tap"
git config user.name 'wasync release automation'
git config user.email '41898282+github-actions[bot]@users.noreply.github.com'
git add Casks lib README.md
if ! git diff --cached --quiet; then
  git commit -m 'Update prebuilt wasync release'
  git push origin HEAD:main
fi
