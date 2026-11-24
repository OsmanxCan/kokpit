#!/usr/bin/env bash
# Paneli derler: önce React arayüzü, sonra Go binary'si (arayüz binary'nin içine gömülür).
#
#   ./build.sh                 → bin/kokpit (bu bilgisayarın mimarisi, Linux)
#   ./build.sh hepsi           → bin/kokpit_linux_amd64 ve _arm64
#
# Sürüm git etiketinden (v1.2.3), depo adı git remote'tan otomatik alınır.
# Elle vermek için: VERSION=1.2.3 REPO=kullanici/depo ./build.sh
set -euo pipefail
cd "$(dirname "$0")"

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo dev)}"
VERSION="${VERSION:-dev}"
if [ -z "${REPO:-}" ]; then
  REPO="$(git config --get remote.origin.url 2>/dev/null | sed -E 's#(git@|https://)github.com[:/]##; s#\.git$##' || true)"
fi
LDFLAGS="-s -w -X main.Version=${VERSION} -X main.Repo=${REPO}"

echo "→ Sürüm: ${VERSION}  Depo: ${REPO:-(yok)}"
echo "→ Arayüz derleniyor (web/)"
( cd web && { [ -d node_modules ] || npm ci --no-audit --no-fund; } && npm run build )

mkdir -p bin
if [ "${1:-}" = "hepsi" ]; then
  for arch in amd64 arm64; do
    echo "→ linux/${arch}"
    CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -ldflags "$LDFLAGS" -o "bin/kokpit_linux_${arch}" .
  done
  ( cd bin && sha256sum kokpit_linux_* > checksums.txt )
else
  echo "→ Go binary derleniyor"
  CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "$LDFLAGS" -o bin/kokpit .
fi
ls -lh bin/
echo "✓ Hazır"
