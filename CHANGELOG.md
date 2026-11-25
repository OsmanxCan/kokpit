# Değişiklik Günlüğü

Bu dosyadaki her `## [sürüm]` bölümü, GitHub Release notu olarak otomatik kullanılır.
Biçim: [Keep a Changelog](https://keepachangelog.com/tr-TR/1.1.0/), sürümleme: [SemVer](https://semver.org/lang/tr/).

## [0.3.0] - 2026-11-25

### Eklendi
- **Uptime izleme:** Sitelere düzenli aralıklarla (30 sn – 30 dk) istek atılır; durum kodu ve isteğe bağlı "sayfada şu metin geçmeli" kontrolü. Son 24 saat saatlik şerit, 24 saat / 7 gün / 30 gün erişilebilirlik, yanıt süresi grafiği ve kesinti geçmişi. Nginx'teki siteler tek tıkla eklenebiliyor
- Yeni bildirimler: **site çöktü** (art arda 2 başarısız kontrol), **site düzeldi** (kesinti süresiyle), **SSL sertifikası bitiyor** (14 gün kala)
- **Firewall sayfası:** UFW kurallarını listeleme, ekleme (izin ver / engelle / sınırla, port veya aralık, kaynak IP/ağ, açıklama) ve silme; firewall'u açma / kapatma
  - Açarken SSH portuna izin veren bir kural yoksa engellenir
  - **Otomatik geri alma:** Firewall açıldıktan sonra seçilen süre (2/5/10 dk) içinde "her şey çalışıyor" onayı gelmezse kendiliğinden kapanır; yanlış kural yüzünden sunucuya erişimin kesilmesini önler
  - SSH kuralları silinirken ayrıca uyarılır; silme sırasında kural numarası değişmişse işlem reddedilir
- **Saldırı haritası:** Başarısız SSH denemelerinin ülkelere göre dünya haritası, ülke listesi, saatlik/günlük grafik ve en çok deneyen IP'ler (fail2ban'ın banladıkları işaretli). Ülke bilgisi sunucuya indirilen DB-IP Lite veritabanından bulunur, IP'ler dışarı gönderilmez
- Genel bakışta izlenen sitelerin durumu ve erişilemeyen siteler
- **Dosyalar:** İzin verdiğin klasörlerde dosya gezgini. Gezinme, klasör/dosya oluşturma, yeniden adlandırma, taşıma, silme, metin dosyalarını düzenleme (Ctrl+S, başkası değiştirdiyse üzerine yazmaz), resim/video/ses önizleme, dosya ve klasörleri (zip olarak) indirme
  - **Erişim izinleri:** "Klasöre erişim ver" ile sunucunun klasör ağacında gezip (sadece klasör adları görünür) bir klasör seçilir; okuma her zaman, **yazma** ve **silme** ayrı ayrı verilir. İzinler sonradan düzenlenebilir veya kaldırılabilir. Hassas konumlarda (/root, /home, /etc altı…) uyarı gösterilir
  - Varsayılan olarak `/srv/kokpit-depo` erişime açık gelir (panel root değilse, örn. kendi bilgisayarında, veri klasöründeki `depo`)
  - Sürükle bırak ile yükleme; büyük dosyalar 50 MB'lık parçalarla gönderilir (Cloudflare'in 100 MB sınırına takılmaz), bağlantı koparsa kaldığı yerden devam eder. Aynı adlı dosyanın üzerine yazmak silme izni ister
  - Klasörün dışına `..` veya sembolik bağlantı ile çıkılamaz; `/`, `/etc`, `/usr` gibi sistem klasörlerinin tamamına izin verilemez
  - Yeni dosyalar bulunduğu klasörün sahibine ait olur; düzenlenen dosyanın izinleri korunur
- **Web terminali:** Panelden sunucuda kabuk (xterm, tam renk, Türkçe karakter). 15 dakika işlem yapılmazsa kapanır, panel oturumu bitince kapanır, aynı anda en fazla 5 terminal. Ayarlar'dan kapatılabilir
- Yeni bildirim: **web terminali açıldı** (kim, hangi IP'den)
- Tüm dosya işlemleri ve terminal oturumları işlem kayıtlarına yazılır
- **Deploy:** Projeleri tek tıkla güncelleme. Sırasıyla: git'ten dalın en yeni sürümünü çeker, tanımlı adımları (npm ci, build, migrate…) çalıştırır, PM2 uygulamasını yeniden başlatır ve isteğe bağlı olarak siteye istek atıp yanıt verdiğini kontrol eder
  - Çıktı canlı izlenir, istenirse durdurulabilir; her projenin son 30 deploy'u ve çıktıları saklanır
  - **Otomatik geri alma:** bir adım veya sağlık kontrolü başarısız olursa önceki sürüme dönülüp adımlar yeniden çalıştırılır. Geçmişten istenen herhangi bir sürüme de elle dönülebilir
  - Sunucudaki klasörde commit edilmemiş değişiklik veya uzakta olmayan commit varsa üzerine yazmamak için deploy durur
  - Komutlar klasörün sahibi olan kullanıcıyla çalışır (isteğe bağlı başka kullanıcı seçilebilir); nvm ile kurulmuş Node otomatik bulunur
  - Node.js, pnpm, Laravel ve Go için hazır adım şablonları
  - Yeni bildirimler: **deploy tamamlandı**, **deploy başarısız**
- **Telegram komutları** (isteğe bağlı, varsayılan kapalı): `/durum`, `/servisler`, `/siteler`, `/deploy`, `/yenidenbaslat`
  - Sadece ayarlardaki Chat ID'den gelen mesajlara yanıt verilir; diğerleri sessizce yok sayılır
  - Deploy ve yeniden başlatma önce onay butonu ister; onay tek kullanımlık ve 2 dakika geçerli. Sadece "Telegram'dan deploy edilebilir" işaretli projeler listelenir; serbest komut çalıştırılamaz
  - Deploy'un hangi adımda olduğu ve sonucu aynı mesajda güncellenir

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
