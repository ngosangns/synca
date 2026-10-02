#!/usr/bin/env bash
# Build darwin-arm64 asset locally and optionally publish with gh.
# Usage:
#   ./scripts/release-local.sh           # build only
#   ./scripts/release-local.sh --publish # create/upload GitHub release for Cargo.toml version
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

VER="$(grep -m1 '^version' Cargo.toml | sed -E 's/.*"([^"]+)".*/\1/')"
TAG="v${VER}"
ASSET="synca-v${VER}-darwin-arm64"
OUTDIR="${ROOT}/dist"
mkdir -p "$OUTDIR"

echo "Building release binary (version ${VER})..."
cargo build --release -p synca
cp target/release/synca "${OUTDIR}/${ASSET}"
chmod +x "${OUTDIR}/${ASSET}"
shasum -a 256 "${OUTDIR}/${ASSET}" | awk '{print $1}' > "${OUTDIR}/${ASSET}.sha256"
echo "Wrote ${OUTDIR}/${ASSET}"
echo "SHA256=$(cat "${OUTDIR}/${ASSET}.sha256")"

if [[ "${1:-}" == "--publish" ]]; then
  if gh release view "$TAG" >/dev/null 2>&1; then
    echo "Release ${TAG} exists; uploading assets..."
    gh release upload "$TAG" "${OUTDIR}/${ASSET}" "${OUTDIR}/${ASSET}.sha256" --clobber
  else
    echo "Creating release ${TAG}..."
    gh release create "$TAG" "${OUTDIR}/${ASSET}" "${OUTDIR}/${ASSET}.sha256" \
      --title "synca ${TAG}" \
      --notes "Local publish of ${TAG} (darwin-arm64)."
  fi
  echo "Published: https://github.com/ngosangns/synca/releases/tag/${TAG}"
else
  echo "Dry build only. Pass --publish to create GitHub release ${TAG}."
fi
