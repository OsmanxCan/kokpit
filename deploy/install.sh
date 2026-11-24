#!/usr/bin/env bash
# Kokpit kurulum / güncelleme scripti
#
# Tek komutla kurulum (en son GitHub sürümü):
#   curl -fsSL https://github.com/__REPO__/releases/latest/download/install.sh -o install.sh && sudo bash install.sh
#
# Yanında "kokpit" dosyası varsa onu kurar (kendi derlediğin sürüm için).
# Zaten kuruluysa sadece binary'yi günceller, ayarlara ve kullanıcılara dokunmaz.
set -euo pipefail

REPO="${REPO:-__REPO__}"
BIN=/usr/local/bin/kokpit
ETC=/etc/kokpit
DATA=/var/lib/kokpit
UNIT=/etc/systemd/system/kokpit.service
HERE="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"

renk() { printf '\033[%sm%s\033[0m\n' "$1" "$2"; }
adim() { renk "1;33" "→ $*"; }
hata() { renk "31" "✗ $*"; exit 1; }

[ "$(id -u)" -eq 0 ] || hata "root olarak çalıştır: sudo bash install.sh"
[ "$(uname -s)" = "Linux" ] || hata "Sadece Linux destekleniyor"
command -v systemctl >/dev/null || hata "systemd gerekli"

case "$(uname -m)" in
  x86_64 | amd64) ARCH=amd64 ;;
  aarch64 | arm64) ARCH=arm64 ;;
  *) hata "Desteklenmeyen mimari: $(uname -m)" ;;
esac
ASSET="kokpit_linux_${ARCH}"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

indir() { curl -fsSL --retry 3 -o "$2" "$1"; }

# ---------- binary ----------
if [ -f "$HERE/kokpit" ]; then
  adim "Yerel binary kullanılıyor: $HERE/kokpit"
  cp "$HERE/kokpit" "$TMP/kokpit"
else
  [[ "$REPO" == *"__"* ]] && hata "Depo adı bilinmiyor. REPO=kullanici/depo bash install.sh şeklinde çalıştır."
  command -v curl >/dev/null || hata "curl gerekli: apt install curl"
  URL="https://github.com/$REPO/releases/latest/download"
  adim "İndiriliyor: $URL/$ASSET"
  indir "$URL/$ASSET" "$TMP/kokpit"
  indir "$URL/checksums.txt" "$TMP/checksums.txt"
  BEKLENEN="$(awk -v f="$ASSET" '$2==f || $2=="*"f {print $1}' "$TMP/checksums.txt")"
  GELEN="$(sha256sum "$TMP/kokpit" | awk '{print $1}')"
  [ -n "$BEKLENEN" ] && [ "$BEKLENEN" = "$GELEN" ] || hata "SHA-256 doğrulaması başarısız!"
  renk 32 "✓ SHA-256 doğrulandı"
fi
chmod 755 "$TMP/kokpit"
"$TMP/kokpit" surum >/dev/null || hata "binary çalışmıyor (mimari uyumsuz olabilir)"

GUNCELLEME=0
[ -f "$BIN" ] && [ -f "$UNIT" ] && GUNCELLEME=1

[ -f "$BIN" ] && cp -a "$BIN" "$BIN.eski"
install -m 755 "$TMP/kokpit" "$BIN"
renk 32 "✓ $("$BIN" surum | head -1) kuruldu → $BIN"

if [ "$GUNCELLEME" = 1 ]; then
  systemctl restart kokpit
  sleep 2
  systemctl is-active --quiet kokpit && renk 32 "✓ Güncellendi ve yeniden başlatıldı" || hata "Servis başlamadı: journalctl -u kokpit -n 50 (eski sürüm: $BIN.eski)"
  exit 0
fi

# ---------- ilk kurulum ----------
adim "Ayarlar: $ETC/config.env"
install -d -m 700 "$ETC" "$DATA"
if [ ! -f "$ETC/config.env" ]; then
  if [ -f "$HERE/config.env.example" ]; then
    install -m 600 "$HERE/config.env.example" "$ETC/config.env"
  elif [[ "$REPO" != *"__"* ]]; then
    indir "https://github.com/$REPO/releases/latest/download/config.env.example" "$TMP/config.env"
    install -m 600 "$TMP/config.env" "$ETC/config.env"
  else
    hata "config.env.example bulunamadı"
  fi
fi

adim "systemd servisi"
if [ -f "$HERE/kokpit.service" ]; then
  install -m 644 "$HERE/kokpit.service" "$UNIT"
else
  indir "https://github.com/$REPO/releases/latest/download/kokpit.service" "$TMP/unit"
  install -m 644 "$TMP/unit" "$UNIT"
fi
systemctl daemon-reload

if command -v apt-get >/dev/null && ! command -v qrencode >/dev/null; then
  adim "QR kod aracı (2FA kurulumu için)"
  apt-get install -y -qq qrencode >/dev/null 2>&1 || true
fi

if [ ! -s "$DATA/users.json" ]; then
  echo
  adim "Panel kullanıcısı oluştur (şifre + 2FA)"
  set -a; . "$ETC/config.env"; set +a
  if [ -r /dev/tty ]; then "$BIN" kullanici-ekle </dev/tty; else "$BIN" kullanici-ekle; fi
fi

systemctl enable --now kokpit >/dev/null 2>&1
echo -n "  Panelin açılması bekleniyor"
HAZIR=0
for _ in $(seq 1 30); do
  if curl -fsS -o /dev/null --max-time 2 http://127.0.0.1:3100/ 2>/dev/null; then HAZIR=1; break; fi
  echo -n "."; sleep 1
done
echo
if [ "$HAZIR" = 1 ]; then
  renk 32 "✓ Panel çalışıyor: http://127.0.0.1:3100 (sadece sunucunun içinden erişilebilir)"
else
  renk 31 "✗ Panel yanıt vermiyor: journalctl -u kokpit -n 50"
fi

cat <<'TXT'

Sıradaki adımlar:
  1) Sunucuyu sertleştir:   sudo bash sertlestir.sh
  2) Cloudflare Tunnel ile panel.alanadin.com → http://127.0.0.1:3100
  3) Cloudflare Access ile sadece kendi e-postana izin ver
  4) Telegram bildirimleri: /etc/kokpit/config.env
Ayrıntılar: README.md → Kurulum
TXT
