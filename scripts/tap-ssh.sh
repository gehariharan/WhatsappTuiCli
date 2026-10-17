#!/usr/bin/env bash
# Source this file from a release job. Its temporary credentials are cleaned by the caller.
if [[ -z "${HOMEBREW_TAP_SSH_KEY:-}" ]]; then
  echo "Missing repository-scoped HOMEBREW_TAP_SSH_KEY" >&2
  return 1
fi
tap_ssh_dir="$(mktemp -d)"
chmod 700 "$tap_ssh_dir"
printf '%s\n' "$HOMEBREW_TAP_SSH_KEY" > "$tap_ssh_dir/key"
chmod 600 "$tap_ssh_dir/key"
# Get GitHub's host keys over its authenticated HTTPS API, not unauthenticated keyscan.
gh api meta --jq '.ssh_keys[] | "github.com " + .' > "$tap_ssh_dir/known_hosts"
printf -v GIT_SSH_COMMAND 'ssh -i %q -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=%q' "$tap_ssh_dir/key" "$tap_ssh_dir/known_hosts"
export GIT_SSH_COMMAND

# Homebrew deliberately sanitizes GIT_SSH_COMMAND; its supported override is a config path.
cat > "$tap_ssh_dir/config" <<CONFIG
Host github.com
  HostName github.com
  User git
  IdentityFile "$tap_ssh_dir/key"
  IdentitiesOnly yes
  StrictHostKeyChecking yes
  UserKnownHostsFile "$tap_ssh_dir/known_hosts"
CONFIG
export HOMEBREW_SSH_CONFIG_PATH="$tap_ssh_dir/config"
