# Güvenlik Politikası

## Açık bildirme

Bu panel sunucuda **root** olarak çalışır, bu yüzden güvenlik açıkları ciddi sonuçlar doğurabilir.

Bir açık bulursan lütfen **herkese açık issue açma**. Bunun yerine GitHub'daki
**Security → Report a vulnerability** (özel güvenlik bildirimi) özelliğini kullan.

Bildirimine şunları ekle:
- Etkilenen sürüm (`kokpit surum`)
- Açığın nasıl tetiklendiği (adım adım)
- Olası etki

Bildirimlere en geç 7 gün içinde dönüş yapılır. Düzeltme yayınlanana kadar ayrıntılar gizli tutulur.

## Desteklenen sürümler

Sadece en son sürüm güvenlik düzeltmesi alır. Panelin **Güncellemeler** sayfasından ya da
`sudo kokpit guncelle` ile güncel kal.

## Önerilen kullanım

- Paneli **asla** doğrudan internete açma (`PANEL_ADDR` her zaman `127.0.0.1` olmalı)
- Önüne Cloudflare Access, VPN veya benzeri bir kimlik doğrulama katmanı koy
- 2FA sırrını ve `/var/lib/kokpit` klasörünü kimseyle paylaşma
