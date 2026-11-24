# Değişiklik Günlüğü

Bu dosyadaki her `## [sürüm]` bölümü, GitHub Release notu olarak otomatik kullanılır.
Biçim: [Keep a Changelog](https://keepachangelog.com/tr-TR/1.1.0/), sürümleme: [SemVer](https://semver.org/lang/tr/).

## [0.2.0] - 2026-11-24

### Eklendi
- **Ayarlar sayfası:** Telegram bağlantısı, uyarı eşikleri, izlenen servisler, oturum ve güncelleme ayarları artık panelden yapılıyor; terminale gerek yok
- **Mesaj şablonları:** 16 bildirim türünün her biri ayrı ayrı açılıp kapatılabiliyor, bekleme süresi ve mesaj şablonu düzenlenebiliyor. Değişkenler (`{{servis}}`, `{{ip}}`…), kalın/italik biçim ve Telegram görünümünde canlı önizleme
- **Hesap:** Şifre değiştirme ve 2FA'yı yeniden kurma (QR panelde görünür)
- **Kullanıcılar:** Panelden kullanıcı ekleme ve silme
- Açık / koyu / sistem teması
- Kenar çubuğu daraltılabiliyor (sol üstteki düğme veya Ctrl+B); tercih hatırlanıyor

### Değişti
- Arayüz shadcn/ui tarzında yeniden tasarlandı (dashboard-01 düzeni: inset kenar çubuğu, bölüm kartları, etkileşimli kaynak grafiği); yazı tipi Geist
- Bildirim şablonu düzenleyicisi Command penceresi düzeninde; küçük ekranlarda kaydırılabiliyor
- `config.env`'deki Telegram, eşik ve servis ayarları ilk açılışta panele aktarılır, sonrasında panelden yönetilir

## [0.1.2] - 2026-11-24

### Düzeltildi
- Güvenlik kontrolleri hem "kurulu" hem "kurulu değil" gibi çelişkili görünüyordu: başlıklar artık sadece neyin kontrol edildiğini, yanındaki metin gerçek durumu gösteriyor
- Fail2ban artık sadece kurulu olup olmadığına değil, servisin gerçekten çalışıp çalışmadığına da bakılarak kontrol ediliyor

## [0.1.1] - 2026-11-24

### Düzeltildi
- Panel açılırken SSH geçmişini taradığı için büyük log'lu sunucularda HTTP sunucusu geç başlıyordu; tarama artık arka planda
- Kurulum scripti panelin açılmasını 30 saniyeye kadar bekliyor
- Çok sayıda SSH saldırısı alan sunucularda bellek kullanımı 200 MB'a çıkıyordu: SSH log'ları artık satır satır okunuyor ve `journalctl --grep` ile önceden süzülüyor
- Go bellek sınırı 64 MB olarak ayarlandı

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
