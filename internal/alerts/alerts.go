// Package alerts kuralları kontrol eder ve Telegram'a bildirim gönderir.
package alerts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/OsmanxCan/kokpit/internal/config"
	"github.com/OsmanxCan/kokpit/internal/security"
	"github.com/OsmanxCan/kokpit/internal/services"
	"github.com/OsmanxCan/kokpit/internal/store"
	"github.com/OsmanxCan/kokpit/internal/system"
)

type Engine struct {
	cfg      *config.Config
	st       *store.Store
	sys      *system.Collector
	svc      *services.Manager
	mu       sync.Mutex
	cooldown map[string]time.Time
	highCPU  int // art arda yüksek CPU ölçüm sayısı
	highRAM  int
	lastSSH  time.Time
	pm2State map[string]string
}

func New(cfg *config.Config, st *store.Store, sys *system.Collector, svc *services.Manager) *Engine {
	return &Engine{cfg: cfg, st: st, sys: sys, svc: svc, cooldown: map[string]time.Time{}, pm2State: map[string]string{}}
}

func (e *Engine) TelegramReady() bool { return e.cfg.TelegramToken != "" && e.cfg.TelegramChat != "" }

func (e *Engine) send(text string) error {
	if !e.TelegramReady() {
		return fmt.Errorf("Telegram ayarlı değil")
	}
	host := e.sys.Latest().Hostname
	body := url.Values{
		"chat_id":    {e.cfg.TelegramChat},
		"text":       {"🖥 " + host + "\n" + text},
		"parse_mode": {"HTML"},
	}
	cl := &http.Client{Timeout: 10 * time.Second}
	resp, err := cl.PostForm("https://api.telegram.org/bot"+e.cfg.TelegramToken+"/sendMessage", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var r struct{ Description string }
		json.NewDecoder(resp.Body).Decode(&r)
		return fmt.Errorf("Telegram: %s", r.Description)
	}
	return nil
}

// Fire bir uyarı oluşturur. Aynı anahtar için cooldown süresince tekrar göndermez.
func (e *Engine) Fire(level, kind, key, msg string, cooldown time.Duration) {
	e.mu.Lock()
	if t, ok := e.cooldown[key]; ok && time.Since(t) < cooldown {
		e.mu.Unlock()
		return
	}
	e.cooldown[key] = time.Now()
	e.mu.Unlock()
	icon := map[string]string{"critical": "🔴", "warning": "🟡", "info": "🔵", "ok": "🟢"}[level]
	err := e.send(icon + " " + msg)
	e.st.AddAlert(level, kind, msg, err == nil)
}

func (e *Engine) Test() error {
	err := e.send("✅ Test bildirimi: Kokpit bildirimleri çalışıyor.")
	e.st.AddAlert("info", "test", "Test bildirimi", err == nil)
	return err
}

// Start arka planda çalışır; panelin açılışını bekletmez.
func (e *Engine) Start() {
	e.lastSSH = time.Now()
	go func() {
		// İlk açılışta son 30 günün SSH IP'lerini "bilinen" olarak işaretle (sahte alarm olmasın).
		// Büyük journal'larda uzun sürebileceği için HTTP sunucusunu bekletmez.
		if e.st.Get("ssh_seeded") == "" {
			for ip := range security.SSHAcceptedIPs("-30d") {
				e.st.AddKnownIP(ip)
			}
			e.st.Set("ssh_seeded", "1")
		}
		e.Fire("ok", "panel", "panel_start", "Kokpit başlatıldı.", time.Minute)
		time.Sleep(20 * time.Second)
		for {
			e.check()
			time.Sleep(60 * time.Second)
		}
	}()
}

func (e *Engine) check() {
	s := e.sys.Latest()
	// CPU / RAM: 3 dakika boyunca yüksekse
	if s.CPU >= e.cfg.CPUAlert {
		e.highCPU++
	} else {
		e.highCPU = 0
	}
	if e.highCPU >= 3 {
		e.Fire("warning", "cpu", "cpu", fmt.Sprintf("CPU kullanımı 3 dakikadır yüksek: <b>%%%.0f</b>", s.CPU), 30*time.Minute)
	}
	if s.MemPct >= e.cfg.RAMAlert {
		e.highRAM++
	} else {
		e.highRAM = 0
	}
	if e.highRAM >= 3 {
		e.Fire("warning", "ram", "ram", fmt.Sprintf("RAM kullanımı 3 dakikadır yüksek: <b>%%%.0f</b>", s.MemPct), 30*time.Minute)
	}
	for _, d := range s.Disks {
		if d.Percent >= e.cfg.DiskAlert {
			e.Fire("critical", "disk", "disk"+d.Mount, fmt.Sprintf("Disk dolmak üzere: <b>%s</b> %%%.0f dolu", d.Mount, d.Percent), 6*time.Hour)
		}
	}

	// PM2 süreçleri
	if items, err := e.svc.PM2(); err == nil {
		for _, it := range items {
			prev := e.pm2State[it.Name]
			e.pm2State[it.Name] = it.Status
			if !it.Healthy {
				e.Fire("critical", "pm2", "pm2"+it.Name, fmt.Sprintf("PM2 süreci çalışmıyor: <b>%s</b> (%s)", it.Name, it.Status), 15*time.Minute)
			} else if prev != "" && prev != "online" {
				e.Fire("ok", "pm2", "pm2ok"+it.Name, fmt.Sprintf("PM2 süreci tekrar çalışıyor: <b>%s</b>", it.Name), time.Minute)
			}
		}
	}

	// İzlenen systemd servisleri
	if items, err := e.svc.Systemd(false); err == nil {
		for _, it := range items {
			if it.User == "izleniyor" && !it.Healthy {
				e.Fire("critical", "systemd", "sd"+it.Name, fmt.Sprintf("Servis çalışmıyor: <b>%s</b> (%s)", it.Name, it.Status), 15*time.Minute)
			}
		}
	}

	// Docker container'ları
	if items, err := e.svc.Docker(); err == nil {
		for _, it := range items {
			if !it.Healthy {
				e.Fire("warning", "docker", "dk"+it.Name, fmt.Sprintf("Container çalışmıyor: <b>%s</b> (%s)", it.Name, it.Status), time.Hour)
			}
		}
	}

	// Yeni SSH girişleri
	since := e.lastSSH.Add(-5 * time.Second).Format("2006-01-02 15:04:05")
	e.lastSSH = time.Now()
	fails := map[string]int{}
	for _, ev := range security.SSHLog(since) {
		switch ev.Type {
		case "accepted":
			if !e.st.IsKnownIP(ev.IP) {
				e.st.AddKnownIP(ev.IP)
				e.Fire("critical", "ssh", "sshnew"+ev.IP, fmt.Sprintf("⚠️ <b>YENİ IP'den SSH girişi!</b>\nKullanıcı: %s\nIP: %s\nYöntem: %s\nSen değilsen hemen müdahale et.", ev.User, ev.IP, ev.Method), time.Minute)
			} else {
				e.Fire("info", "ssh", "sshok"+ev.IP+ev.User, fmt.Sprintf("SSH girişi: %s @ %s (%s)", ev.User, ev.IP, ev.Method), 30*time.Minute)
			}
		case "failed", "invalid":
			fails[ev.IP]++
		}
	}
	total := 0
	var top []string
	for ip, n := range fails {
		total += n
		if n >= 10 {
			top = append(top, fmt.Sprintf("%s (%d)", ip, n))
		}
	}
	if total >= 30 {
		e.Fire("warning", "ssh", "sshbrute", fmt.Sprintf("Son dakikada %d başarısız SSH denemesi. Yoğun IP'ler: %s", total, strings.Join(top, ", ")), time.Hour)
	}
}
