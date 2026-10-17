#!/usr/bin/env bash
# Run on the signing Mac. The private keys and notary credentials stay there.
set -euo pipefail
version="${1:?Usage: scripts/sign-release-macos.sh VERSION ASSET_DIRECTORY}"
assets="${2:?Usage: scripts/sign-release-macos.sh VERSION ASSET_DIRECTORY}"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9][A-Za-z0-9.-]*)?$ ]]; then
  echo 'Invalid release version' >&2; exit 1
fi
scripts_dir="$(cd "$(dirname "$0")" && pwd)"
assets="$(cd "$assets" && pwd)"
: "${WASYNC_CODESIGN_IDENTITY:?Set the Developer ID Application identity}"
: "${WASYNC_INSTALLER_IDENTITY:?Set the Developer ID Installer identity}"
: "${WASYNC_NOTARY_PROFILE:?Set the notarytool profile}"
: "${WASYNC_SIGNING_KEYCHAIN:?Set the signing Keychain path}"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# Unlock and use credentials in this same SSH session. Password input is handled
# by security's terminal prompt, never an argument or a script variable.
security unlock-keychain "$WASYNC_SIGNING_KEYCHAIN"
xcrun notarytool history --keychain-profile "$WASYNC_NOTARY_PROFILE" \
  --keychain "$WASYNC_SIGNING_KEYCHAIN" --output-format json > "$work/history.json"
for architecture in arm64 amd64; do
  name="wasync_${version}_darwin_${architecture}"
  archive="$assets/$name.tar.gz"
  package="$assets/$name.pkg"
  mkdir "$work/$architecture"
  tar -xzf "$archive" -C "$work/$architecture"
  binary="$work/$architecture/wasync"
  expected_cpu="$architecture"
  if [[ "$architecture" == amd64 ]]; then expected_cpu=x86_64; fi
  if [[ "$(lipo -archs "$binary")" != "$expected_cpu" ]]; then
    echo "Unexpected executable architecture: $name" >&2; exit 1
  fi
  # Confirm archive and installer contain identical original executables.
  pkgutil --expand-full "$package" "$work/$architecture-pkg"
  payload="$work/$architecture-pkg/Payload"
  cmp "$binary" "$payload/usr/local/bin/wasync"
  codesign --force --options runtime --timestamp \
    --keychain "$WASYNC_SIGNING_KEYCHAIN" \
    --identifier com.gehariharan.wasynctuicli \
    --sign "$WASYNC_CODESIGN_IDENTITY" "$binary"
  install -m 755 "$binary" "$payload/usr/local/bin/wasync"
  pkgbuild --root "$payload" --scripts "$work/$architecture-pkg/Scripts" \
    --identifier com.gehariharan.wasynctuicli --version "${version%%-*}" \
    --install-location / --ownership recommended \
    --keychain "$WASYNC_SIGNING_KEYCHAIN" \
    --sign "$WASYNC_INSTALLER_IDENTITY" --timestamp "$work/$name.pkg"
  bash "$scripts_dir/notarize.sh" "$WASYNC_NOTARY_PROFILE" "$binary" "$work/$name.pkg"
  spctl --assess --type execute --verbose=2 "$binary"
  tar -czf "$work/$name.tar.gz" -C "$work/$architecture" wasync README.md THIRD_PARTY_LICENSES
done
# Replace inputs only after both architectures pass signing and notarization.
for architecture in arm64 amd64; do
  name="wasync_${version}_darwin_${architecture}"
  cp "$work/$name.tar.gz" "$work/$name.pkg" "$assets/"
  (cd "$assets"; shasum -a 256 "$name.tar.gz" > "$name.tar.gz.sha256"; shasum -a 256 "$name.pkg" > "$name.pkg.sha256")
done
printf 'Signed and notarized both macOS architectures in %s\n' "$assets"
