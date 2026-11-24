#!/usr/bin/env bash
# GitHub depo adını projedeki tüm dosyalara yazar ve GitHub Actions dosyalarını
# .github/workflows/ klasörüne taşır. İlk commit'ten önce bir kere çalıştırman yeterli.
#   ./depo-ayarla.sh kullaniciadin/kokpit
set -euo pipefail
# Türkçe yerel ayarda (tr_TR) [A-Z] gibi aralıklar farklı davranabiliyor; C yerel ayarı kullan.
export LC_ALL=C
cd "$(dirname "$0")"
REPO="${1:?Kullanım: ./depo-ayarla.sh kullanici/depo   (örnek: ./depo-ayarla.sh osmancan/kokpit)}"
# Tam adres de kabul edilir: https://github.com/kullanici/depo(.git), git@github.com:kullanici/depo.git
REPO="$(printf '%s' "$REPO" | sed -E 's#^(https?://)?(www\.)?github\.com/##; s#^git@github\.com:##; s#\.git$##; s#/+$##')"
if ! printf '%s' "$REPO" | grep -Eq '^[[:alnum:]-]+/[[:alnum:]_.-]+$'; then
  echo "Geçersiz depo adı: '$REPO'"
  echo "Biçim: kullaniciadi/depoadi  (örnek: osmancan/kokpit)"
  echo "GitHub kullanıcı adında sadece İngilizce harf, rakam ve tire (-) olabilir; Türkçe karakter ve boşluk olamaz."
  exit 1
fi
echo "Depo: $REPO"

# Belgelerdeki yer tutucu
grep -rl "KULLANICI/kokpit" --include="*.md" . 2>/dev/null | grep -v node_modules | while read -r f; do
  sed -i "s#KULLANICI/kokpit#${REPO}#g" "$f"; echo "  güncellendi: $f"
done

# Go modül yolu
ESKI="$(awk '/^module /{print $2}' go.mod)"
YENI="github.com/${REPO}"
if [ "$ESKI" != "$YENI" ]; then
  grep -rl "$ESKI" --include="*.go" --include="go.mod" . | while read -r f; do
    sed -i "s#${ESKI}#${YENI}#g" "$f"
  done
  echo "  Go modülü: $ESKI → $YENI"
fi
# GitHub Actions dosyalarını yerine koy
if [ -d github-workflows ]; then
  mkdir -p .github/workflows
  mv github-workflows/*.yml .github/workflows/
  rmdir github-workflows
  echo "  GitHub Actions → .github/workflows/"
fi
echo "✓ Tamam. Kontrol: go build ./... && git status"
