// Package alerts olayları izler ve bildirimleri (panel + Telegram) gönderir.
// Hangi olayın açık olduğu, bekleme süreleri ve mesaj şablonları panelden ayarlanır.
package alerts

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/OsmanxCan/kokpit/internal/security"
	"github.com/OsmanxCan/kokpit/internal/services"
	"github.com/OsmanxCan/kokpit/internal/settings"
	"github.com/OsmanxCan/kokpit/internal/store"
	"github.com/OsmanxCan/kokpit/internal/system"
)

type Engine struct {
	set      *settings.Store
	st       *store.Store
	sys      *system.Collector
	svc      *services.Manager
	Version  string
	mu       sync.Mutex
	cooldown map[string]time.Time
	highCPU  int // art arda yüksek CPU ölçümü (dakika)
	highRAM  int
	lastSSH  time.Time
	pm2State map[string]string
}

func New(set *settings.Store, st *store.Store, sys *system.Collector, svc *services.Manager) *Engine {
	return &Engine{set: set, st: st, sys: sys, svc: svc, cooldown: map[string]time.Time{}, pm2State: map[string]string{}}
}

func (e *Engine) TelegramReady() bool {
	t := e.set.Get().Telegram
	return t.Token != "" && t.ChatID != ""
}

func (e *Engine) host() string { return e.sys.Latest().Hostname }

// SendTelegram verilen token/chat ile mesaj gönderir (ayarlar sayfasındaki test için de kullanılır).
func SendTelegram(token, chatID, text string) error {
	if token == "" || chatID == "" {
		return errors.New("Telegram token veya chat id girilmemiş")
	}
	body := url.Values{"chat_id": {chatID}, "text": {text}, "parse_mode": {"HTML"}, "disable_web_page_preview": {"true"}}
	cl := &http.Client{Timeout: 10 * time.Second}
	resp, err := cl.PostForm("https://api.telegram.org/bot"+token+"/sendMessage", body)
	if err != nil {
		return fmt.Errorf("Telegram'a ulaşılamadı: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var r struct{ Description string }
		json.NewDecoder(resp.Body).Decode(&r)
		switch {
		case resp.StatusCode == 404 || resp.StatusCode == 401:
			return errors.New("token geçersiz: BotFather'dan aldığın token'ın tamamını (rakamlar:harfler) gir")
		case strings.Contains(r.Description, "chat not found"):
			return errors.New("chat bulunamadı: chat id'yi kontrol et ve bota Telegram'dan /start yaz")
		case strings.Contains(r.Description, "can't parse entities"):
			return errors.New("şablondaki HTML hatalı: " + r.Description)
		}
		return fmt.Errorf("Telegram: %s", r.Description)
	}
	return nil
}

func (e *Engine) compose(def settings.EventDef, ev settings.Event, vars map[string]string) string {
	s := e.set.Get()
	tpl := ev.Template
	if strings.TrimSpace(tpl) == "" {
		tpl = def.Template
	}
	msg := settings.Render(tpl, vars, e.host())
	if s.Telegram.Prefix {
		msg = "🖥 <b>" + settings.Render("{{sunucu}}", nil, e.host()) + "</b>\n" + msg
	}
	return msg
}

// Fire bir olayı tetikler. key aynı olayın farklı örneklerini ayırır (örn. servis adı);
// aynı key için bekleme süresi dolmadan tekrar gönderilmez.
func (e *Engine) Fire(eventID, key string, vars map[string]string) {
	def, ok := settings.EventByID(eventID)
	if !ok {
		return
	}
	s := e.set.Get()
	ev := s.Events[eventID]
	if !ev.Enabled {
		return
	}
	ck := eventID + ":" + key
	e.mu.Lock()
	if t, ok := e.cooldown[ck]; ok && time.Since(t) < time.Duration(ev.Cooldown)*time.Minute {
		e.mu.Unlock()
		return
	}
	e.cooldown[ck] = time.Now()
	e.mu.Unlock()

	msg := e.compose(def, ev, vars)
	sent := false
	if ev.Telegram && s.Telegram.Token != "" && s.Telegram.ChatID != "" {
		sent = SendTelegram(s.Telegram.Token, s.Telegram.ChatID, msg) == nil
	}
	e.st.AddAlert(def.Level, def.Name, settings.Plain(settings.Render(orDefault(ev.Template, def.Template), vars, e.host())), sent)
}

func orDefault(a, b string) string {
	if strings.TrimSpace(a) == "" {
		return b
	}
	return a
}

// SendTest bir olayın şablonunu örnek değerlerle Telegram'a gönderir.
// template boşsa kayıtlı şablon kullanılır.
func (e *Engine) SendTest(eventID, template string) error {
	def, ok := settings.EventByID(eventID)
	if !ok {
		return errors.New("bilinmeyen olay")
	}
	s := e.set.Get()
	ev := s.Events[eventID]
	if template != "" {
		ev.Template = template
	}
	msg := e.compose(def, ev, settings.SampleVars(def))
	err := SendTelegram(s.Telegram.Token, s.Telegram.ChatID, msg)
	e.st.AddAlert("info", "Test: "+def.Name, settings.Plain(settings.Render(orDefault(ev.Template, def.Template), settings.SampleVars(def), e.host())), err == nil)
	return err
}

// Preview şablonun panelde gösterilecek Telegram HTML'ini döner.
func (e *Engine) Preview(eventID, template string) (string, error) {
	def, ok := settings.EventByID(eventID)
	if !ok {
		return "", errors.New("bilinmeyen olay")
	}
	ev := e.set.Get().Events[eventID]
	ev.Template = template
	return e.compose(def, ev, settings.SampleVars(def)), nil
}

// Start arka planda çalışır; panelin açılışını bekletmez.
func (e *Engine) Start() {
	e.lastSSH = time.Now()
	go func() {
		// İlk açılışta son 30 günün SSH IP'lerini "bilinen" olarak işaretle (sahte alarm olmasın).
		if e.st.Get("ssh_seeded") == "" {
			for ip := range security.SSHAcceptedIPs("-30d") {
				e.st.AddKnownIP(ip)
			}
			e.st.Set("ssh_seeded", "1")
		}
		e.Fire("panel_start", "", map[string]string{"surum": e.Version})
		time.Sleep(20 * time.Second)
		for {
			e.check()
			time.Sleep(60 * time.Second)
		}
	}()
}

func pct(v float64) string { return fmt.Sprintf("%%%.0f", v) }

func gb(b uint64) string {
	return strings.Replace(fmt.Sprintf("%.1f GB", float64(b)/(1<<30)), ".", ",", 1)
}

func (e *Engine) check() {
	s := e.sys.Latest()
	set := e.set.Get()
	th := set.Thresholds

	if s.CPU >= th.CPU {
		e.highCPU++
	} else {
		e.highCPU = 0
	}
	if e.highCPU >= th.CPUMinutes {
		e.Fire("cpu_high", "", map[string]string{"cpu": pct(s.CPU), "dakika": fmt.Sprint(e.highCPU), "esik": fmt.Sprintf("%.0f", th.CPU)})
	}
	if s.MemPct >= th.RAM {
		e.highRAM++
	} else {
		e.highRAM = 0
	}
	if e.highRAM >= th.RAMMinutes {
		e.Fire("ram_high", "", map[string]string{"ram": pct(s.MemPct), "kullanilan": gb(s.MemUsed), "toplam": gb(s.MemTotal),
			"dakika": fmt.Sprint(e.highRAM), "esik": fmt.Sprintf("%.0f", th.RAM)})
	}
	for _, d := range s.Disks {
		if d.Percent >= th.Disk {
			e.Fire("disk_full", d.Mount, map[string]string{"disk": d.Mount, "yuzde": pct(d.Percent), "bos": gb(d.Total - d.Used)})
		}
	}

	if items, err := e.svc.PM2(); err == nil {
		for _, it := range items {
			prev := e.pm2State[it.Name]
			e.pm2State[it.Name] = it.Status
			if !it.Healthy {
				e.Fire("pm2_down", it.Name, map[string]string{"surec": it.Name, "durum": it.Status})
			} else if prev != "" && prev != "online" {
				e.Fire("pm2_up", it.Name, map[string]string{"surec": it.Name})
			}
		}
	}

	if items, err := e.svc.Systemd(false); err == nil {
		for _, it := range items {
			if it.User == "izleniyor" && !it.Healthy {
				e.Fire("service_down", it.Name, map[string]string{"servis": it.Name, "durum": it.Status})
			}
		}
	}

	if items, err := e.svc.Docker(); err == nil {
		for _, it := range items {
			if !it.Healthy {
				e.Fire("docker_down", it.Name, map[string]string{"container": it.Name, "durum": it.Status})
			}
		}
	}

	since := e.lastSSH.Add(-5 * time.Second).Format("2006-01-02 15:04:05")
	e.lastSSH = time.Now()
	fails := map[string]int{}
	for _, ev := range security.SSHLog(since) {
		switch ev.Type {
		case "accepted":
			vars := map[string]string{"kullanici": ev.User, "ip": ev.IP, "yontem": ev.Method}
			if !e.st.IsKnownIP(ev.IP) {
				e.st.AddKnownIP(ev.IP)
				e.Fire("ssh_new_ip", ev.IP, vars)
			} else {
				e.Fire("ssh_login", ev.IP+ev.User, vars)
			}
		case "failed", "invalid":
			fails[ev.IP]++
		}
	}
	total := 0
	type ipc struct {
		ip string
		n  int
	}
	var list []ipc
	for ip, n := range fails {
		total += n
		list = append(list, ipc{ip, n})
	}
	if total >= th.SSHBrute {
		sort.Slice(list, func(i, j int) bool { return list[i].n > list[j].n })
		var top []string
		for i, x := range list {
			if i == 5 {
				break
			}
			top = append(top, fmt.Sprintf("%s (%d)", x.ip, x.n))
		}
		e.Fire("ssh_brute", "", map[string]string{"sayi": fmt.Sprint(total), "ipler": strings.Join(top, ", ")})
	}
}
