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

**Şifreni veya telefonunu kaybedersen** sunucuda tekrar çalıştır. Eski oturumlar kapanır:

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

1. Telegram'da **@BotFather** → `/newbot` → bir ad ver → **token**'ı kopyala
2. Yeni botuna herhangi bir mesaj gönder
3. Tarayıcıda `https://api.telegram.org/bot<TOKEN>/getUpdates` adresini aç → `"chat":{"id":123456789` → bu sayı **chat id**
4. `/etc/kokpit/config.env` dosyasına yaz:
   ```
   TELEGRAM_TOKEN=123456:ABC...
   TELEGRAM_CHAT_ID=123456789
   ```
5. `sudo systemctl restart kokpit` → panelde **Bildirimler → Test bildirimi gönder**

| Seviye | Durum |
|---|---|
| 🔴 | PM2 süreci veya izlenen servis çöktü · disk %90'ı geçti · **daha önce görülmemiş IP'den SSH girişi** |
| 🟡 | CPU/RAM 3 dakikadır yüksek · Docker container durdu · dakikada 30+ başarısız SSH denemesi · panele 3+ hatalı giriş |
| 🔵 | Panele giriş yapıldı · yeni panel sürümü yayında |
| 🟢 | Çöken süreç tekrar ayağa kalktı · panel güncellendi |

Aynı uyarı tekrar tekrar gönderilmez, her türün bir bekleme süresi var.

## 7. Ayarlar

`/etc/kokpit/config.env`. Değişiklikten sonra `sudo systemctl restart kokpit`.

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `PANEL_ADDR` | `127.0.0.1:3100` | Dinlenen adres. **Dışarıya açma.** |
| `PANEL_DATA` | `/var/lib/kokpit` | Kullanıcılar, oturumlar, metrik geçmişi |
| `PANEL_SESSION_IP_BIND` | `true` | Oturumu IP'ye bağla (IP değişirse tekrar giriş ister) |
| `PANEL_PM2_BIN` | otomatik | pm2 yolu (nvm kurulumları otomatik bulunur) |
| `PM2_HOME` | — | PM2 ana klasörü, örn. `/root/.pm2` |
| `PANEL_WATCH_SERVICES` | `nginx,ssh,docker,cloudflared,fail2ban,pm2-root` | Çökerse bildirim gönderilecek servisler |
| `PANEL_UPDATE_CHECK` | `true` | GitHub'dan sürüm kontrolü |
| `PANEL_REPO` | derlemeden gelir | Güncellemelerin alınacağı depo (fork'lar için) |
| `TELEGRAM_TOKEN`, `TELEGRAM_CHAT_ID` | — | Telegram bildirimleri |
| `ALERT_CPU`, `ALERT_RAM`, `ALERT_DISK` | `90` | Uyarı eşikleri (%) |

## 8. Sık karşılaşılan durumlar

**Panel açılmıyor**
```bash
sudo systemctl status kokpit
sudo journalctl -u kokpit -n 50
curl -I http://127.0.0.1:3100       # sunucunun içinden
```

**"Oturum yok" diyip sürekli girişe atıyor.** Mobil veride IP sık değişiyor olabilir. `PANEL_SESSION_IP_BIND=false` yap.

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
