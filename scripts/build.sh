#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-0.0.1-dev}"
COMMIT="${GIT_COMMIT:-unknown}"
if [ "$COMMIT" = "unknown" ] && git rev-parse --short HEAD >/dev/null 2>&1; then
  COMMIT="$(git rev-parse --short HEAD)"
fi
BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
LDFLAGS="-s -w -X wslc-bridge/internal/buildinfo.Version=${VERSION} -X wslc-bridge/internal/buildinfo.Commit=${COMMIT} -X wslc-bridge/internal/buildinfo.Date=${BUILD_DATE}"

mkdir -p dist
for ARCH in amd64 arm64; do
  OUTPUT="dist/wslc-bridge-windows-${ARCH}.exe"
  printf 'Building %s\n' "$OUTPUT"
  GOOS=windows GOARCH="$ARCH" CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$OUTPUT" ./cmd/wslc-bridge
done

python3 - "$VERSION" "$COMMIT" "$BUILD_DATE" <<'PY'
import hashlib
import json
import pathlib
import sys

version, commit, build_date = sys.argv[1:]
root = pathlib.Path("dist")
artifacts = []
checksums = []
for arch in ("amd64", "arm64"):
    path = root / f"wslc-bridge-windows-{arch}.exe"
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    artifacts.append({"os": "windows", "arch": arch, "file": path.name, "sha256": digest})
    checksums.append(f"{digest}  {path.name}")
(root / "checksums.txt").write_text("\n".join(checksums) + "\n", encoding="utf-8")
(root / "bridge-manifest.json").write_text(
    json.dumps({"schemaVersion": 1, "version": version, "commit": commit, "buildDate": build_date, "artifacts": artifacts}, indent=2) + "\n",
    encoding="utf-8",
)
PY

printf 'Artifacts and checksums are in %s/dist\n' "$ROOT"
