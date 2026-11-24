// Package config panel ayarlarını ortam değişkenlerinden okur.
// systemd servisi bu değişkenleri /etc/kokpit/config.env dosyasından yükler.
package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr          string   // Panelin dinleyeceği adres. Dışarıya açma! (127.0.0.1:3100)
	DataDir       string   // SQLite veritabanının duracağı klasör
	Dev           bool     // Geliştirme modu: Secure cookie kapalı
	SessionIPBind bool     // Oturumu giriş yapılan IP'ye bağla
	PM2Bin        string   // pm2 yolu (boşsa otomatik bulunur)
	PM2Home       string   // PM2_HOME (root için /root/.pm2)
	WatchServices []string // Çökerse bildirim gönderilecek systemd servisleri
	TelegramToken string
	TelegramChat  string
	Repo          string  // güncelleme kontrolü için GitHub deposu (kullanici/depo)
	UpdateCheck   bool    // GitHub'dan yeni sürüm kontrolü
	CPUAlert      float64 // yüzde
	RAMAlert      float64
	DiskAlert     float64
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := strings.ToLower(env(key, ""))
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "evet" || v == "yes"
}

func envFloat(key string, def float64) float64 {
	if f, err := strconv.ParseFloat(env(key, ""), 64); err == nil {
		return f
	}
	return def
}

// Load ayarları okur. defaultRepo derleme sırasında gömülen depo adıdır.
func Load(defaultRepo string) *Config {
	c := &Config{
		Addr:          env("PANEL_ADDR", "127.0.0.1:3100"),
		DataDir:       env("PANEL_DATA", "/var/lib/kokpit"),
		Dev:           envBool("PANEL_DEV", false),
		SessionIPBind: envBool("PANEL_SESSION_IP_BIND", true),
		PM2Bin:        env("PANEL_PM2_BIN", ""),
		PM2Home:       env("PM2_HOME", ""),
		TelegramToken: env("TELEGRAM_TOKEN", ""),
		TelegramChat:  env("TELEGRAM_CHAT_ID", ""),
		Repo:          env("PANEL_REPO", defaultRepo),
		UpdateCheck:   envBool("PANEL_UPDATE_CHECK", true),
		CPUAlert:      envFloat("ALERT_CPU", 90),
		RAMAlert:      envFloat("ALERT_RAM", 90),
		DiskAlert:     envFloat("ALERT_DISK", 90),
	}
	for _, s := range strings.Split(env("PANEL_WATCH_SERVICES", "nginx,ssh,docker,cloudflared,fail2ban,pm2-root"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			c.WatchServices = append(c.WatchServices, s)
		}
	}
	return c
}
