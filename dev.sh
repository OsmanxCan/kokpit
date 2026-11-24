#!/usr/bin/env bash
# Geliştirme modu: Go backend (3100) + Vite arayüzü (5173, anında yenilenir).
# Tarayıcıda http://localhost:5173 aç. Durdurmak için Ctrl+C.
#
# İlk çalıştırmada kullanıcı oluşturur. Veriler ./.veri klasöründe durur.
set -euo pipefail
cd "$(dirname "$0")"

export PANEL_DEV=1 PANEL_DATA=./.veri PANEL_ADDR=127.0.0.1:3100 PANEL_UPDATE_CHECK=false PANEL_SESSION_IP_BIND=false

[ -d web/node_modules ] || (cd web && npm ci --no-audit --no-fund)
[ -f web/dist/index.html ] || (cd web && npm run build)

if [ ! -s .veri/users.json ]; then
  echo "İlk çalıştırma: panel kullanıcısı oluştur"
  go run . kullanici-ekle
fi

go run -ldflags "-X main.Version=dev" . &
GO_PID=$!
trap 'kill $GO_PID 2>/dev/null' EXIT
cd web && npm run dev
