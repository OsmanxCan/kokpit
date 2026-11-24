#!/usr/bin/env bash
# Derlenmiş paneli sunucuya gönderir ve yeniden başlatır.
# Kullanım: ./yukle.sh root@SUNUCU_IP
#   İlk kurulumda: ./yukle.sh root@SUNUCU_IP --kurulum
set -euo pipefail
cd "$(dirname "$0")"
HEDEF="${1:?Kullanım: ./yukle.sh root@SUNUCU_IP [--kurulum]}"

[ -f bin/kokpit ] || ./build.sh

echo "→ Dosyalar gönderiliyor: $HEDEF"
ssh "$HEDEF" "mkdir -p /root/kokpit-kurulum"
scp bin/kokpit deploy/install.sh deploy/sertlestir.sh deploy/kokpit.service deploy/config.env.example \
  "$HEDEF:/root/kokpit-kurulum/"

if [ "${2:-}" = "--kurulum" ]; then
  echo "→ Kurulum başlatılıyor (etkileşimli)"
  ssh -t "$HEDEF" "bash /root/kokpit-kurulum/install.sh"
else
  echo "→ Güncelleniyor"
  ssh "$HEDEF" "install -m 755 /root/kokpit-kurulum/kokpit /usr/local/bin/kokpit && systemctl restart kokpit && sleep 1 && systemctl --no-pager --lines=3 status kokpit"
fi
