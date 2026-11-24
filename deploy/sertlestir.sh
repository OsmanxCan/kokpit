#!/usr/bin/env bash
# Sunucu sertleştirme scripti — Ubuntu / Debian
#
# Her adım ayrı sorulur, hiçbir şey sormadan değiştirilmez.
# SSH ve firewall adımlarında "ölü adam anahtarı" vardır: 5 dakika içinde onay
# vermezsen değişiklik OTOMATİK GERİ ALINIR, sunucunun dışında kalmazsın.
#
# Kullanım: sudo bash sertlestir.sh
set -uo pipefail

[ "$(id -u)" -eq 0 ] || { echo "root olarak çalıştır"; exit 1; }
command -v apt-get >/dev/null || { echo "Bu script apt kullanan sistemler (Ubuntu/Debian) içindir."; exit 1; }

YEDEK="/root/sertlestir-yedek-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$YEDEK"
SSH_IP="${SSH_CLIENT%% *}"
SSH_PORT="$(sshd -T 2>/dev/null | awk '$1=="port"{print $2; exit}')"
SSH_PORT="${SSH_PORT:-22}"

k() { printf '\n\033[1;33m== %s ==\033[0m\n' "$*"; }
ok() { printf '\033[32m✓ %s\033[0m\n' "$*"; }
uyar() { printf '\033[31m! %s\033[0m\n' "$*"; }
sor() { local c; read -r -p "$1 [e/H] " c </dev/tty; [[ "$c" =~ ^[eEyY] ]]; }
sor_evet() { local c; read -r -p "$1 [E/h] " c </dev/tty; [[ ! "$c" =~ ^[hHnN] ]]; }

echo "Yedekler: $YEDEK"
[ -n "$SSH_IP" ] && echo "Şu anki SSH bağlantın: $SSH_IP (port $SSH_PORT)"

# ------------------------------------------------------------------ 1. SSH
k "1/5  SSH: şifreyle girişi kapat, root sadece anahtarla"
KEYS=0
for f in /root/.ssh/authorized_keys /home/*/.ssh/authorized_keys; do
  [ -s "$f" ] && KEYS=$((KEYS + $(grep -cE '^(ssh-|ecdsa-|sk-)' "$f" || true)))
done
if [ "$KEYS" -eq 0 ]; then
  uyar "Hiçbir kullanıcıda SSH anahtarı yok. Bu adım atlanıyor."
  echo "  Önce kendi bilgisayarında:  ssh-keygen -t ed25519  &&  ssh-copy-id kullanici@SUNUCU_IP"
  echo "  Anahtarla şifresiz girebildiğini test et, sonra bu scripti tekrar çalıştır."
elif ! grep -qiE '^\s*Include\s+/etc/ssh/sshd_config.d/\*\.conf' /etc/ssh/sshd_config; then
  uyar "sshd_config, sshd_config.d klasörünü içermiyor; bu adım atlanıyor (elle yapılmalı)."
else
  echo "  $KEYS SSH anahtarı bulundu."
  echo "  Şu anki ayarlar:"
  sshd -T 2>/dev/null | grep -E '^(permitrootlogin|passwordauthentication|kbdinteractiveauthentication|maxauthtries) ' | sed 's/^/    /'
  if sor "  SSH ayarlarını sıkılaştırayım mı?"; then
    cp -a /etc/ssh "$YEDEK/ssh"
    cat > /etc/ssh/sshd_config.d/00-kokpit.conf <<'CONF'
# sertlestir.sh tarafından eklendi. sshd ilk okuduğu değeri kullanır;
# dosya adı 00- ile başladığı için diğer .conf dosyalarından önce okunur.
PermitRootLogin prohibit-password
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitEmptyPasswords no
MaxAuthTries 3
LoginGraceTime 30
X11Forwarding no
CONF
    SSH_UNIT=ssh; systemctl list-unit-files ssh.service >/dev/null 2>&1 || SSH_UNIT=sshd
    if sshd -t; then
      systemd-run --quiet --unit=sertlestir-ssh-geri --on-active=300 \
        /bin/sh -c "rm -f /etc/ssh/sshd_config.d/00-kokpit.conf; systemctl reload $SSH_UNIT" >/dev/null
      systemctl reload "$SSH_UNIT"
      echo
      echo "  ŞİMDİ: Bu pencereyi KAPATMA. Yeni bir terminalden SSH ile anahtarla gir."
      echo "  5 dakika içinde onaylamazsan ayar kendiliğinden geri alınır."
      if sor "  Yeni terminalden girebildin mi?"; then
        systemctl stop sertlestir-ssh-geri.timer 2>/dev/null
        ok "SSH sıkılaştırıldı."
      else
        rm -f /etc/ssh/sshd_config.d/00-kokpit.conf; systemctl reload "$SSH_UNIT"
        systemctl stop sertlestir-ssh-geri.timer 2>/dev/null
        uyar "SSH ayarı geri alındı."
      fi
    else
      rm -f /etc/ssh/sshd_config.d/00-kokpit.conf
      uyar "sshd ayar testi başarısız, değişiklik geri alındı."
    fi
  fi
fi

# ------------------------------------------------------------------ 2. Fail2ban
k "2/5  Fail2ban: SSH kaba kuvvet saldırılarını otomatik engelle"
if sor_evet "  Fail2ban kurayım mı?"; then
  apt-get update -qq && apt-get install -y -qq fail2ban >/dev/null
  IGNORE="127.0.0.1/8 ::1"
  [ -n "$SSH_IP" ] && IGNORE="$IGNORE $SSH_IP"
  [ -f /etc/fail2ban/jail.local ] && cp -a /etc/fail2ban/jail.local "$YEDEK/"
  cat > /etc/fail2ban/jail.local <<CONF
# sertlestir.sh tarafından eklendi
[DEFAULT]
ignoreip = $IGNORE
bantime = 1h
bantime.increment = true
bantime.maxtime = 1w
findtime = 10m
maxretry = 5

[sshd]
enabled = true
port = $SSH_PORT
backend = systemd
CONF
  systemctl enable fail2ban >/dev/null 2>&1
  systemctl restart fail2ban
  sleep 2
  if fail2ban-client status sshd >/dev/null 2>&1; then
    ok "Fail2ban çalışıyor (beyaz listedeki IP'n: ${SSH_IP:-yok})."
  else
    uyar "fail2ban başlatılamadı: journalctl -u fail2ban"
  fi
fi

# ------------------------------------------------------------------ 3. UFW
k "3/5  Firewall (UFW): sadece gereken portlar açık kalsın"
command -v ufw >/dev/null || apt-get install -y -qq ufw >/dev/null
echo "  Dışarıya açık dinleyen portlar algılanıyor…"
declare -a KURALLAR=()
declare -A GORULDU=()
WEB_VAR=0
while read -r proto _state _rq _sq local _peer rest; do
  addr="${local%:*}"; port="${local##*:}"
  case "$addr" in 0.0.0.0 | '*' | '[::]' | '::') ;; *) continue ;; esac
  proc="$(grep -oP 'users:\(\("\K[^"]+' <<<"$rest" || true)"
  key="$port/$proto"
  [ -n "${GORULDU[$key]:-}" ] && continue
  GORULDU[$key]=1
  if [ "$port" = "$SSH_PORT" ] && [ "$proto" = "tcp" ]; then continue; fi
  if { [ "$port" = 80 ] || [ "$port" = 443 ]; } && [ "$proto" = "tcp" ]; then WEB_VAR=1; continue; fi
  case "$proc" in
    docker-proxy) varsayilan=e; not="Docker (zaten UFW'yi atlar)" ;;
    openvpn | wireguard* | "") varsayilan=e; not="VPN/çekirdek" ;;
    node | next-server* | python* | php* | java | dotnet) varsayilan=h; not="uygulama: Nginx arkasındaysa kapat" ;;
    *) varsayilan=h; not="" ;;
  esac
  printf '    %-12s %-18s %s\n' "$key" "${proc:-çekirdek}" "$not"
  KURALLAR+=("$key|$varsayilan|${proc:-çekirdek}")
done < <(ss -tulpnH 2>/dev/null | awk '$1=="udp" || $2=="LISTEN"')

echo
echo "  Plan:"
echo "    • Gelen trafik varsayılan KAPALI, giden açık"
echo "    • SSH ($SSH_PORT/tcp) açık, deneme hızı sınırlı"
echo "    • VPN ve container arayüzlerinden gelen trafik açık"
echo "    • Yönlendirme (forward) politikası ACCEPT (VPN internet çıkışı bozulmasın)"

declare -a AC=()
for k_ in "${KURALLAR[@]}"; do
  IFS='|' read -r key def proc <<<"$k_"
  if [ "$def" = e ]; then
    sor_evet "    $key ($proc) açık kalsın mı?" && AC+=("$key|$proc")
  else
    sor "    $key ($proc) açık kalsın mı?" && AC+=("$key|$proc")
  fi
done

WEB=0
if [ "$WEB_VAR" = 1 ]; then
  echo
  echo "  80/443 (web) kime açık olsun?"
  echo "   1) Sadece Cloudflare (önerilen). ŞART: tüm domainler Cloudflare'de turuncu bulutta (proxy) olmalı."
  echo "   2) Herkese"
  read -r -p "  Seçim [1/2]: " WEB </dev/tty
fi

if sor "  UFW kurallarını uygulayayım mı?"; then
  cp -a /etc/ufw "$YEDEK/ufw" 2>/dev/null
  ufw --force reset >/dev/null
  sed -i 's/^DEFAULT_FORWARD_POLICY=.*/DEFAULT_FORWARD_POLICY="ACCEPT"/' /etc/default/ufw
  ufw default deny incoming >/dev/null
  ufw default allow outgoing >/dev/null
  ufw limit "$SSH_PORT/tcp" comment 'SSH' >/dev/null
  for a in "${AC[@]}"; do
    IFS='|' read -r key proc <<<"$a"
    ufw allow "$key" comment "$proc" >/dev/null
  done
  for IF in $(ip -o link show | awk -F': ' '{print $2}' | cut -d@ -f1 | grep -E '^(wg|tun|tap|docker|br-|amn)' || true); do
    ufw allow in on "$IF" comment "ic ag $IF" >/dev/null
  done
  if [ "$WEB" = "2" ]; then
    ufw allow 80/tcp comment 'HTTP' >/dev/null
    ufw allow 443/tcp comment 'HTTPS' >/dev/null
  elif [ "$WEB" = "1" ]; then
    CF4=$(curl -fsS --max-time 10 https://www.cloudflare.com/ips-v4 || true)
    CF6=$(curl -fsS --max-time 10 https://www.cloudflare.com/ips-v6 || true)
    [ -z "$CF4" ] && CF4="173.245.48.0/20 103.21.244.0/22 103.22.200.0/22 103.31.4.0/22 141.101.64.0/18 108.162.192.0/18 190.93.240.0/20 188.114.96.0/20 197.234.240.0/22 198.41.128.0/17 162.158.0.0/15 104.16.0.0/13 104.24.0.0/14 172.64.0.0/13 131.0.72.0/22"
    [ -z "$CF6" ] && CF6="2400:cb00::/32 2606:4700::/32 2803:f800::/32 2405:b500::/32 2405:8100::/32 2a06:98c0::/29 2c0f:f248::/32"
    for n in $CF4 $CF6; do
      ufw allow proto tcp from "$n" to any port 80,443 comment 'Cloudflare' >/dev/null
    done
    ok "80/443 sadece Cloudflare IP'lerine açıldı."
  fi
  systemd-run --quiet --unit=sertlestir-ufw-geri --on-active=300 /usr/sbin/ufw disable >/dev/null
  ufw --force enable
  echo
  echo "  ŞİMDİ yeni bir terminalden SSH ile girmeyi, sitelerini ve VPN'ini test et."
  echo "  5 dakika içinde onaylamazsan firewall kendiliğinden kapanır."
  if sor "  Her şey çalışıyor mu?"; then
    systemctl stop sertlestir-ufw-geri.timer 2>/dev/null
    ok "Firewall aktif."
    ufw status numbered | head -50
  else
    ufw disable; systemctl stop sertlestir-ufw-geri.timer 2>/dev/null
    uyar "Firewall kapatıldı. Kurallar duruyor; sorunu bulunca 'ufw enable' ile açabilirsin."
  fi
fi

# ------------------------------------------------------------------ 4. Uygulama portları
k "4/5  Tüm arayüzlerde dinleyen uygulamalar"
APP=$(ss -tlnpH 2>/dev/null | awk '$4 ~ /^(0\.0\.0\.0|\*|\[::\]):/' | grep -E '"(node|next-server[^"]*|python[^"]*|php[^"]*|java|dotnet|ruby|gunicorn|uvicorn)"' || true)
if [ -z "$APP" ]; then
  ok "Uygulama sunucusu dışarıya doğrudan açık değil."
else
  echo "$APP" | awk '{print "    " $4 "  " $6}'
  cat <<'TXT'
  Bu uygulamalar Nginx/Cloudflare'i atlayıp IP:port ile doğrudan erişilebilir olabilir.
  Firewall bunu engelliyor ama ek bir kat olarak sadece 127.0.0.1'de dinletmen önerilir:
    Next.js:  next start -H 127.0.0.1         (PM2: pm2 start npm --name app -- start -- -H 127.0.0.1)
    Node:     app.listen(port, '127.0.0.1')
    Laravel:  php artisan serve --host=127.0.0.1
    Docker:   -p 127.0.0.1:3000:3000
TXT
fi

# ------------------------------------------------------------------ 5. Otomatik güncellemeler
k "5/5  Otomatik güvenlik güncellemeleri"
if [ -f /etc/apt/apt.conf.d/20auto-upgrades ] && grep -q 'Unattended-Upgrade "1"' /etc/apt/apt.conf.d/20auto-upgrades; then
  ok "Zaten açık."
elif sor_evet "  Otomatik güvenlik güncellemelerini açayım mı?"; then
  apt-get install -y -qq unattended-upgrades >/dev/null
  printf 'APT::Periodic::Update-Package-Lists "1";\nAPT::Periodic::Unattended-Upgrade "1";\n' > /etc/apt/apt.conf.d/20auto-upgrades
  ok "Açıldı."
fi

k "Bitti"
echo "Yedekler: $YEDEK"
echo "Panelde Güvenlik → 'Şimdi tara' ile yeni skoru görebilirsin."
