# Geliştirme

## Gereksinimler

| | Arch / CachyOS | Ubuntu / Debian |
|---|---|---|
| Go 1.24+ | `sudo pacman -S go` | [go.dev/dl](https://go.dev/dl) |
| Node.js 20+ ve npm | `sudo pacman -S nodejs npm` | `sudo apt install nodejs npm` |
| git | `sudo pacman -S git` | `sudo apt install git` |
| (isteğe bağlı) qrencode | `sudo pacman -S qrencode` | `sudo apt install qrencode` |

## Kendi bilgisayarında çalıştırma

```bash
./dev.sh
```

Script şunları yapar:

1. `web/node_modules` yoksa `npm ci` çalıştırır
2. İlk çalıştırmada panel kullanıcısı oluşturur: şifre ve 2FA. QR kodu telefonla okut ya da anahtarı elle gir
3. Go backend'i `127.0.0.1:3100`'de başlatır
4. Vite arayüzünü başlatır → **http://localhost:5173**

Arayüzde yaptığın değişiklikler anında yansır. Go kodunu değiştirdiysen `Ctrl+C` ile durdurup `./dev.sh`'ı yeniden çalıştır.

Geliştirme verileri (kullanıcı, oturumlar, 2FA sırrı) `./.veri/` klasöründe durur ve git'e eklenmez. Sıfırlamak için klasörü sil.

**Bilmen gerekenler**

- Panel bu modda **senin bilgisayarının** verilerini gösterir: kendi CPU'n, diskin, süreçlerin.
- Bazı bilgiler root yetkisi ister: SSH logları, `sshd -T`, ufw, fail2ban. Normal kullanıcıyla çalışınca bu bölümler boş ya da "kurulu değil" görünür, bu normaldir. Tam görmek için:
  ```bash
  ./build.sh && sudo PANEL_DEV=1 PANEL_DATA=./.veri ./bin/kokpit
  # → http://127.0.0.1:3100
  ```
- `pm2`, `docker` veya `nginx` yoksa ilgili bölümler boş kalır.
- `PANEL_DEV=1` sadece geliştirme içindir: çerezin HTTPS olmadan çalışmasını sağlar. Sunucuda kullanma.

## Sadece derleme

```bash
./build.sh          # bin/kokpit (bu makine)
./build.sh hepsi    # bin/kokpit_linux_amd64 + _arm64 + checksums.txt
```

Kendi derlediğin sürümü bir sunucuya göndermek için:

```bash
./yukle.sh root@SUNUCU_IP --kurulum   # ilk kurulum
./yukle.sh root@SUNUCU_IP             # güncelleme
```

## Proje yapısı

```
main.go                 giriş noktası ve komutlar (baslat, kullanici-ekle, guncelle, surum)
internal/
  config/               ayarlar (ortam değişkenleri)
  store/                veri saklama (JSON dosyaları, atomik yazma)
  crypt/                şifre hash (PBKDF2-SHA256) ve TOTP 2FA
  system/               CPU, RAM, disk, ağ, süreçler (/proc ve /sys'ten)
  services/             PM2, Docker, systemd yönetimi ve canlı log
  nginx/                site listesi, SSL bitiş tarihleri
  security/             güvenlik kontrolleri, SSH log analizi, portlar, fail2ban
  alerts/               uyarı kuralları ve Telegram
  update/               GitHub Releases kontrolü ve kendini güncelleme
  server/               HTTP API, oturum, CSRF, güvenlik başlıkları
web/                    React + Vite + Tailwind arayüzü (derlenince binary'ye gömülür)
  src/pages/            sayfalar
  src/components/       ortak bileşenler (grafikler SVG ile, dış kütüphane yok)
deploy/                 install.sh, sertlestir.sh, systemd servisi, örnek ayar
.github/workflows/      CI ve otomatik sürüm yayını
```

## Yeni sürüm yayınlama

Sürümler [SemVer](https://semver.org/lang/tr/) kullanır: `MAJOR.MINOR.PATCH`.

1. `CHANGELOG.md` dosyasına yeni bir bölüm ekle:
   ```markdown
   ## [0.2.0] - 2026-12-01
   ### Eklendi
   - ...
   ```
2. Commit at ve etiketle:
   ```bash
   git add -A && git commit -m "0.2.0"
   git tag v0.2.0
   git push && git push --tags
   ```
3. GitHub Actions (`.github/workflows/release.yml`) şunları yapar:
   - testleri çalıştırır
   - amd64 ve arm64 binary'lerini derler (sürüm ve depo adı içine gömülür)
   - `checksums.txt` oluşturur
   - CHANGELOG'daki ilgili bölümle bir GitHub Release yayınlar
4. Kurulu paneller en geç 6 saat içinde yeni sürümü görür.

`v0.3.0-beta.1` gibi tire içeren etiketler **pre-release** olarak yayınlanır. Paneller bunları güncelleme olarak görmez, çünkü GitHub'ın "latest" sürümü pre-release'leri atlar.

## Katkı

Pull request'ler açık. Göndermeden önce:

```bash
gofmt -l .          # boş çıktı vermeli
go vet ./... && go test ./...
cd web && npm run build
```
