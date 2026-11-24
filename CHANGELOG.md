# Değişiklik Günlüğü

Bu dosyadaki her `## [sürüm]` bölümü, GitHub Release notu olarak otomatik kullanılır.
Biçim: [Keep a Changelog](https://keepachangelog.com/tr-TR/1.1.0/), sürümleme: [SemVer](https://semver.org/lang/tr/).

## [0.1.0] - 2026-11-24

İlk sürüm.

### Eklendi
- Sistem: CPU, RAM, swap, disk, ağ, load, sıcaklık; canlı ve 7 günlük grafikler; süreç listesi
- Projeler: PM2, Docker ve systemd yönetimi (başlat, durdur, yeniden başlat) ve canlı log
- Siteler: Nginx domainleri, SSL bitiş tarihleri, yanıt süresi kontrolü, `nginx -t` ve reload
- Güvenlik: 0–100 skor, SSH saldırı analizi, açık portlar, UFW durumu, fail2ban ile IP engelleme
- Telegram bildirimleri (servis çökmesi, disk, CPU/RAM, yeni IP'den SSH girişi, hatalı panel girişi)
- İşlem kayıtları ve oturum yönetimi
- Şifre + TOTP 2FA, giriş deneme sınırı, IP'ye bağlı oturumlar
- GitHub Releases üzerinden sürüm kontrolü ve panelden / terminalden güncelleme
- `install.sh` ile tek komutla kurulum, `sertlestir.sh` ile sunucu sertleştirme
