# Kurulum Rehberi

Bu rehber, paneli bir sunucuya kurup internetten güvenli şekilde erişilebilir hale getirmeyi adım adım anlatır.

```
Tarayıcı → Cloudflare Access (e-posta doğrulama) → Cloudflare Tunnel → 127.0.0.1:3100 → Panel (şifre + 2FA)
```

1. [SSH anahtarı](#1-ssh-anahtarı)
2. [Paneli kur](#2-paneli-kur)
3. [Sunucuyu sertleştir](#3-sunucuyu-sertleştir)
4. [Cloudflare Tunnel](#4-cloudflare-tunnel)
5. [Cloudflare Access](#5-cloudflare-access)
6. [Telegram bildirimleri](#6-telegram-bildirimleri)
7. [Ayarlar](#7-ayarlar)
8. [Sık karşılaşılan durumlar](#8-sık-karşılaşılan-durumlar)

---

## 1. SSH anahtarı

Sertleştirme adımı SSH'ta şifreyle girişi kapatır. Önce kendi bilgisayarından anahtarla girebildiğinden emin ol:

```bash
ssh-keygen -t ed25519            # anahtarın yoksa
ssh-copy-id root@SUNUCU_IP
ssh root@SUNUCU_IP               # şifre sormadan girmeli
```

## 2. Paneli kur

Sunucuda:

```bash
curl -fsSL https://github.com/OsmanxCan/kokpit/releases/latest/download/install.sh -o install.sh
sudo bash install.sh
```

Kurulum sırasında panel kullanıcısını oluşturursun:

1. Kullanıcı adı ve şifre (en az 12 karakter)
2. Terminalde çıkan QR kodu **Aegis**, **2FAS** veya **Google Authenticator** ile okut
3. Uygulamadaki 6 haneli kodu yaz

Kurulumdan sonra panel `http://127.0.0.1:3100` adresinde çalışır. Bu adrese sadece sunucunun içinden erişilebilir.

**Şifreni değiştirmek veya 2FA'yı yeniden kurmak** için panelde **Ayarlar → Hesabım** sayfasını kullan. **Panele giremiyorsan** (şifreni veya telefonunu kaybettiysen) sunucuda şunu çalıştır; eski oturumlar kapanır:

```bash
sudo kokpit kullanici-ekle
```

## 3. Sunucuyu sertleştir

```bash
curl -fsSL https://github.com/OsmanxCan/kokpit/releases/latest/download/sertlestir.sh -o sertlestir.sh
sudo bash sertlestir.sh
```

Script her adımı ayrı sorar. **SSH ve firewall adımlarında 5 dakikalık otomatik geri alma** var. Değişiklikten sonra yeni bir terminalden girip test et. 5 dakika içinde onaylamazsan değişiklik kendiliğinden geri alınır, yani sunucunun dışında kalmazsın.

| Adım | Ne yapar |
|---|---|
| SSH | Şifreyle giriş kapanır, root sadece anahtarla girer, 3 deneme hakkı |
| Fail2ban | 10 dakikada 5 hatalı SSH denemesi → 1 saat ban (tekrarlarsa süre artar). Senin IP'n beyaz listede |
| UFW | Gelen her şey kapalı. Açık portları algılayıp tek tek sorar. 80/443'ü sadece Cloudflare'e açabilir |
| Uygulamalar | Tüm arayüzlerde dinleyen Node/PHP/Python uygulamalarını gösterir, localhost'a nasıl bağlanacağını anlatır |
| Güncellemeler | Otomatik güvenlik güncellemelerini açar |

> **80/443'ü sadece Cloudflare'e açacaksan**, tüm domainlerinin Cloudflare DNS'te **turuncu bulut (Proxied)** olması gerekir. Gri buluttaki siteler erişilemez olur.

## 4. Cloudflare Tunnel

Tünel, sunucunda port açmadan paneli internete bağlar.

**cloudflared kurulu değilse:**

```bash
curl -fsSL https://pkg.cloudflare.com/cloudflare-main.gpg | sudo tee /usr/share/keyrings/cloudflare-main.gpg >/dev/null
echo "deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] https://pkg.cloudflare.com/cloudflared $(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/cloudflared.list
sudo apt update && sudo apt install cloudflared
cloudflared tunnel login
cloudflared tunnel create sunucu
```

**`/etc/cloudflared/config.yml`:**

```yaml
tunnel: <TUNEL_ID>
credentials-file: /root/.cloudflared/<TUNEL_ID>.json

ingress:
  - hostname: panel.alanadin.com
    service: http://127.0.0.1:3100
  # ... diğer kayıtların ...
  - service: http_status:404     # en sonda kalmalı
```

```bash
cloudflared tunnel route dns <TUNEL_ID> panel.alanadin.com
sudo cloudflared service install      # ilk kez kuruyorsan
sudo systemctl restart cloudflared
```

## 5. Cloudflare Access

**Bu adımı atlama.** Access olmadan panelin giriş sayfası herkese açık olur.

1. [one.dash.cloudflare.com](https://one.dash.cloudflare.com) → **Access → Applications → Add an application → Self-hosted**
2. Application domain: `panel.alanadin.com`
3. Session duration: 24 hours
4. Policy: **Allow** → Include → **Emails** → kendi e-posta adresin
5. Kaydet

Artık panel adresini açınca önce Cloudflare e-postana bir kod gönderir, ardından panelin şifre + 2FA ekranı gelir.

## 6. Telegram bildirimleri

Panelde **Ayarlar → Telegram** sayfasını aç. Sayfadaki adımları izle:

1. Telegram'da **@BotFather** → `/newbot` → bota bir ad ver → verdiği **token**'ın tamamını kopyala
2. Botunun sayfasını açıp **Başlat**'a bas
3. **@userinfobot**'a `/start` yaz → gelen **Id** senin chat id'n
4. Token ve chat id'yi gir → **Test mesajı gönder** → **Kaydet**

Hangi olayların bildirileceğini, tekrar bekleme sürelerini ve mesaj şablonlarını **Ayarlar → Bildirimler** sayfasından değiştirebilirsin. Şablonlarda `{{servis}}`, `{{ip}}`, `{{sunucu}}` gibi değişkenler ve `<b>kalın</b>`, `<i>italik</i>`, `<code>kod</code>` biçimleri kullanılabilir.

| Varsayılan seviye | Olay |
|---|---|
| 🔴 Kritik | PM2 süreci veya izlenen servis çöktü · disk doluyor · **daha önce görülmemiş IP'den SSH girişi** |
| 🟡 Uyarı | CPU/RAM uzun süre yüksek · Docker container durdu · yoğun SSH saldırısı · panele hatalı giriş |
| 🔵 Bilgi | SSH girişi · panele giriş · yeni Kokpit sürümü |
| 🟢 Düzeldi | Çöken süreç tekrar çalışıyor · Kokpit başladı · güncelleme kuruldu |

## 7. Ayarlar

Günlük ayarların hepsi panelde **Ayarlar** sayfasında:

| Sekme | İçerik |
|---|---|
| Genel | CPU / RAM / disk eşikleri, SSH saldırı eşiği, oturumu IP'ye bağlama, güncelleme kontrolü |
| Telegram | Bot token, chat id, bağlantı testi |
| Bildirimler | Her olayı aç/kapa, bekleme süresi, mesaj şablonu ve önizleme |
| İzlenen servisler | Çökünce bildirim gönderilecek systemd servisleri |
| Hesabım | Şifre değiştirme, 2FA'yı yeniden kurma |
| Kullanıcılar | Kullanıcı ekleme ve silme |

Panelden yapılan ayarlar `/var/lib/kokpit/settings.json` dosyasında saklanır.

`/etc/kokpit/config.env` sadece sunucu düzeyindeki ayarlar içindir. Değiştirdikten sonra `sudo systemctl restart kokpit` çalıştır.

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `PANEL_ADDR` | `127.0.0.1:3100` | Dinlenen adres. **Dışarıya açma.** |
| `PANEL_DATA` | `/var/lib/kokpit` | Kullanıcılar, oturumlar, ayarlar, metrik geçmişi |
| `PANEL_PM2_BIN` | otomatik | pm2 yolu (nvm kurulumları otomatik bulunur) |
| `PM2_HOME` | — | PM2 ana klasörü, örn. `/root/.pm2` |
| `PANEL_REPO` | derlemeden gelir | Güncellemelerin alınacağı depo (fork'lar için) |

`TELEGRAM_*`, `ALERT_*`, `PANEL_WATCH_SERVICES`, `PANEL_SESSION_IP_BIND` ve `PANEL_UPDATE_CHECK` değişkenleri sadece ilk açılışta panele aktarılır.

## 8. Sık karşılaşılan durumlar

**Panel açılmıyor**
```bash
sudo systemctl status kokpit
sudo journalctl -u kokpit -n 50
curl -I http://127.0.0.1:3100       # sunucunun içinden
```

**"Oturum yok" diyip sürekli girişe atıyor.** Mobil veride IP sık değişiyor olabilir. **Ayarlar → Genel** sayfasında "Oturumu IP adresine bağla" seçeneğini kapat.

**Güncelleme sonrası panel açılmadı.** Eski sürüm `/usr/local/bin/kokpit.eski` olarak saklanır:
```bash
sudo cp /usr/local/bin/kokpit.eski /usr/local/bin/kokpit && sudo systemctl restart kokpit
```

**Firewall sonrası bir şey bozuldu**
```bash
sudo ufw status numbered      # kuralları gör
sudo ufw allow 1234/udp       # eksik portu aç
sudo ufw disable              # acil durumda tamamen kapat
```

**Kaldırma**
```bash
sudo systemctl disable --now kokpit
sudo rm -f /usr/local/bin/kokpit* /etc/systemd/system/kokpit.service
sudo rm -rf /etc/kokpit /var/lib/kokpit
```
