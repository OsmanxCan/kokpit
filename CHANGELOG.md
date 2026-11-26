# Değişiklik Günlüğü

Bu dosyadaki her `## [sürüm]` bölümü, GitHub Release notu olarak otomatik kullanılır.
Biçim: [Keep a Changelog](https://keepachangelog.com/tr-TR/1.1.0/), sürümleme: [SemVer](https://semver.org/lang/tr/).

## [Yayımlanmamış]

### Eklendi
- **Port tarama tespiti** (Saldırılar → Port taramaları): Nmap, masscan gibi araçlarla yapılan taramalar firewall'un engellediği bağlantılardan tespit edilir; tarama türü (SYN, FIN, NULL, XMAS, UDP), taranan portlar ve ülke gösterilir
  - **İnternet geneli** tarayıcılar (Shodan, Censys, Shadowserver…) ile **seni hedefleyenler** ayrı listelenir. Ters DNS kaydı ileri yönde de doğrulanır; saldırgan sahte bir kayıtla kendini zararsız gösteremez
  - Gizlenme amaçlı taramalar (FIN/NULL/XMAS) ve SSH'a da giriş deneyen IP'ler her zaman hedefli sayılır
  - UFW'nin kendi logları dakikada birkaç satırla sınırlı olduğu için Kokpit, engellenen paketler için IP başına sınırlı ayrı bir log kuralı ekler (tespit kapatılınca kaldırılır)
  - **Otomatik engelleme** (varsayılan kapalı): hedefli taramaları firewall'da engeller; süre ayarlanabilir (varsayılan 24 saat, kalıcı da seçilebilir), süresi dolan engeller kendiliğinden kalkar. IP'ler elle de engellenip kaldırılabilir
  - Cloudflare adresleri, özel ağlar, panele giriş yapılan IP'ler ve beyaz listedekiler asla engellenmez
  - Yeni bildirim: **hedefli port taraması**
- **Dinleyen portlar** (Firewall sayfası): sunucuda bağlantı bekleyen her program ve internetten erişilebilir olup olmadığı (dışarı açık / sınırlı / firewall engelliyor / sadece sunucu içi)
  - Docker portlarının UFW kurallarını atladığı ayrıca belirtilir
  - Dışarıya açık olmaması gereken servisler (PostgreSQL, Redis, MongoDB, Docker API…) uyarı olarak gösterilir
  - Yeni bildirim: **dışarıya yeni port açıldı**
- **Web saldırıları** (Saldırılar → Web): Nginx access loglarından (döndürülmüş ve sıkıştırılmış eskiler dahil) saldırı istekleri bulunur: hassas dosya arama (.env, .git, yedekler), SQL injection, dizin dolaşma, XSS, komut çalıştırma denemeleri (Log4Shell vb.), WordPress ve yönetim paneli taramaları, saldırı araçları (sqlmap, nikto, nuclei…), çok fazla 404 üreten IP'ler
  - Hassas bir dosya isteğine 200 dönmüşse Kokpit dosyayı kendisi isteyip içeriğe bakar; gerçekten sızıyorsa kritik uyarı ve bildirim verir (tek sayfa uygulamalarının her yola ana sayfayı döndürmesi yanlış alarm üretmez)
  - Saldıran IP'ler Cloudflare'de, sunucu firewall'unda veya ikisinde birden engellenebilir
  - Yeni bildirim: **hassas dosya dışarıdan okunabiliyor**
- **Nginx gerçek IP ayarı:** Cloudflare Tünel/proxy arkasında Nginx'in ziyaretçi IP'si yerine 127.0.0.1 kaydettiği tespit edilir; tek tıkla `CF-Connecting-IP` ayarı eklenir (sadece tünelden ve Cloudflare'den gelen başlığa güvenilir, `nginx -t` başarısız olursa geri alınır)
- **Cloudflare entegrasyonu** (isteğe bağlı, Ayarlar → Cloudflare): API token doğrulanarak kaydedilir ve tarayıcıya hiç gönderilmez
  - IP'leri tüm alan adlarında süreli veya kalıcı engelleme (süresi dolanlar kendiliğinden kalkar)
  - "Under Attack" modunu panelden veya Telegram'dan (`/saldirimodu`, onay butonlu) açıp kapatma; kapatınca önceki güvenlik seviyesine dönülür
  - Cloudflare'in son 24 saatte engellediği isteklerin listesi
- **Log kuralları** (Güvenlik → Log kuralları): uygulama loglarında (dosya, systemd servisi veya PM2 uygulaması) `<IP>` içeren bir kalıba uyan satırlar izlenir; aynı IP belirlenen sürede eşiği aşarsa Cloudflare'de ve/veya firewall'da engellenir ya da sadece bildirilir
  - Kural kaydedilmeden önce mevcut loglarda denenebilir: kaç satırın eşleştiği, hangi IP'lerin eşiği aştığı görünür
  - Yeni bildirim: **log kuralı IP engelledi**
- **OIDC izleme:** Uptime'da "Kimlik sağlayıcı" türü; keşif belgesi, issuer eşleşmesi ve JWKS'te imza anahtarı olduğu kontrol edilir
- **Yeni site sihirbazı** (Siteler → Yeni site): alan adını ve uygulamanın portunu (veya statik site klasörünü) girince tek seferde yayına alır
  - Nginx site dosyası: siteye özel access/error logları, WebSocket desteği, `.env`/`.git` gibi gizli dosyalara erişim engeli; statik sitelerde tek sayfa uygulaması seçeneği. `nginx -t` ile doğrulanıp yeniden yüklenir
  - Cloudflare Tünel kuralı (yerel olarak yönetilen tünellerde): `config.yml` yedeklenir, `cloudflared ingress validate` ile doğrulanır, tünel yeniden başlatılır; tünel ayağa kalkmazsa eski dosyaya dönülür
  - DNS kaydı: Cloudflare token varsa API ile, yoksa `cloudflared` sertifikası varsa onunla eklenir; ikisi de yoksa eklenecek kayıt gösterilir
  - İsteğe bağlı uptime izleyicisi; ardından tek tıkla deploy projesi ekleme
  - Her değişiklik uygulanmadan önce önizlenir; bir adım başarısız olursa önceki adımlar geri alınır. Sihirbazla oluşturulan siteler sonradan aynı yerden kaldırılabilir (uygulamanın dosyalarına dokunulmaz)
- Saldırılar sayfası üç sekme oldu: SSH, Web, Port taramaları

### Değişti
- Örnek metinler ve testlerdeki alan adları tarafsız hale getirildi (example.com)

### Düzeltildi
- Bir deploy bittikten hemen sonra yenisi başlatılınca nadiren "zaten bir deploy sürüyor" hatası alınması

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
