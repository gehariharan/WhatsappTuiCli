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
trap 'result=$?; if [[ $result == 0 ]]; then rm -rf "$work"; else printf "Signing stopped; diagnostic files retained in %s\n" "$work" >&2; fi' EXIT

# Unlock and use credentials in this same SSH session. Password input is handled
# by security's terminal prompt, never an argument or a script variable.
if [[ "${WASYNC_SIGNING_UNATTENDED:-0}" != 1 ]]; then
  security unlock-keychain "$WASYNC_SIGNING_KEYCHAIN"
fi
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
  # spctl's execute assessment expects an app bundle and rejects standalone
  # Mach-O tools even when notarized. Require Apple's ticket for the CLI itself.
  codesign --verify --strict --verbose=2 -R='notarized' --check-notarization "$binary"
  archive_files=(wasync README.md THIRD_PARTY_LICENSES)
  if [[ -f "$work/$architecture/LICENSE" ]]; then archive_files+=(LICENSE); fi
  tar -czf "$work/$name.tar.gz" -C "$work/$architecture" "${archive_files[@]}"

  axi_name="whatsapp-axi_${version}_darwin_${architecture}"
  mkdir "$work/axi-$architecture"
  tar -xzf "$assets/$axi_name.tar.gz" -C "$work/axi-$architecture"
  axi_binary="$work/axi-$architecture/whatsapp-axi"
  [[ "$(lipo -archs "$axi_binary")" == "$expected_cpu" ]]
  codesign --force --options runtime --timestamp \
    --keychain "$WASYNC_SIGNING_KEYCHAIN" \
    --identifier com.gehariharan.wasynctuicli.axi \
    --sign "$WASYNC_CODESIGN_IDENTITY" "$axi_binary"
  bash "$scripts_dir/notarize.sh" "$WASYNC_NOTARY_PROFILE" "$axi_binary"
  codesign --verify --strict --verbose=2 -R='notarized' --check-notarization "$axi_binary"
  tar -czf "$work/$axi_name.tar.gz" -C "$work/axi-$architecture" whatsapp-axi README.md LICENSE
done
# Replace inputs only after both architectures pass signing and notarization.
for architecture in arm64 amd64; do
  name="wasync_${version}_darwin_${architecture}"
  cp "$work/$name.tar.gz" "$work/$name.pkg" "$assets/"
  cp "$work/whatsapp-axi_${version}_darwin_${architecture}.tar.gz" "$assets/"
  (cd "$assets"; shasum -a 256 "$name.tar.gz" > "$name.tar.gz.sha256"; shasum -a 256 "$name.pkg" > "$name.pkg.sha256")
done
printf 'Signed and notarized both macOS architectures in %s\n' "$assets"
