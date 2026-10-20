#!/usr/bin/env bash
# Test, build the darwin-arm64 CLI, and optionally publish it.
# Usage:
#   ./scripts/release-local.sh           # build only
#   ./scripts/release-local.sh --publish # create/upload the GitHub release for VERSION
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

VER="$(tr -d '[:space:]' < VERSION)"
[[ "$VER" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "VERSION must be X.Y.Z, got '${VER}'" >&2; exit 1; }
SRC_VER="$(sed -n 's/.*Version *= *"\(.*\)"/\1/p' go/internal/core/models.go)"
[[ "$SRC_VER" == "$VER" ]] || { echo "VERSION ${VER} does not match core.Version ${SRC_VER}" >&2; exit 1; }
if [[ "${GITHUB_REF_TYPE:-}" == "tag" && -n "${GITHUB_REF_NAME:-}" && "${GITHUB_REF_NAME}" != "v${VER}" ]]; then
  echo "tag ${GITHUB_REF_NAME} does not match VERSION ${VER}" >&2
  exit 1
fi

TAG="v${VER}"
ASSET="synca-v${VER}-darwin-arm64"
OUTDIR="${ROOT}/dist"
mkdir -p "$OUTDIR"

echo "Testing and building (version ${VER})..."
(cd go && go vet ./... && go test ./... && go build -trimpath -ldflags "-s -w" -o "${OUTDIR}/${ASSET}" ./cmd/synca)
chmod +x "${OUTDIR}/${ASSET}"
printed="$("${OUTDIR}/${ASSET}" --version)"
[[ "$printed" == "synca ${VER}" ]] || { echo "binary printed '${printed}', expected 'synca ${VER}'" >&2; exit 1; }

(
  cd "$OUTDIR"
  shasum -a 256 "$ASSET" > SHA256SUMS
)
echo "Wrote ${OUTDIR}/${ASSET}"
cat "${OUTDIR}/SHA256SUMS"

NOTES_FILE="$(mktemp)"
cat > "$NOTES_FILE" <<NOTES
synca ${TAG}

## Install

After the first install, \`synca update\` replaces \`~/.local/bin/synca\`.

\`\`\`bash
curl -fsSL -o synca "https://github.com/ngosangns/synca/releases/download/${TAG}/${ASSET}"
chmod +x synca
mkdir -p ~/.local/share/synca/bin ~/.local/bin
cp synca ~/.local/share/synca/bin/synca-${VER}
ln -sfn ../share/synca/bin/synca-${VER} ~/.local/bin/synca
\`\`\`

## Asset

- Binary: \`${ASSET}\`
- Checksum: \`SHA256SUMS\` (\`shasum -a 256 -c SHA256SUMS\`)

\`synca update\` verifies the GitHub asset digest, not this checksum file.
NOTES

if [[ "${1:-}" == "--publish" ]]; then
  if gh release view "$TAG" >/dev/null 2>&1; then
    echo "Release ${TAG} exists; uploading assets..."
    gh release upload "$TAG" \
      "${OUTDIR}/${ASSET}" \
      "${OUTDIR}/SHA256SUMS" \
      --clobber
    gh release edit "$TAG" --notes-file "$NOTES_FILE" || true
  else
    echo "Creating release ${TAG}..."
    gh release create "$TAG" \
      "${OUTDIR}/${ASSET}" \
      "${OUTDIR}/SHA256SUMS" \
      --target "$(git rev-parse HEAD)" \
      --title "synca ${TAG}" \
      --notes-file "$NOTES_FILE"
  fi
  echo "Published: https://github.com/ngosangns/synca/releases/tag/${TAG}"
else
  echo "Dry build only. Pass --publish to create GitHub release ${TAG}."
fi
rm -f "$NOTES_FILE"
