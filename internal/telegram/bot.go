// Package telegram Telegram botundan gelen komutları işler (isteğe bağlı, varsayılan kapalı).
//
// Güvenlik:
//   - Sadece ayarlardaki chat id'den gelen mesaj ve butonlar işlenir, diğerleri yok sayılır.
//   - İşlem yapan her komut (deploy, yeniden başlatma) önce onay butonu ister; onay
//     tek kullanımlıktır ve 2 dakika sonra geçersiz olur.
//   - Komutlar sabit bir listeden seçilir; Telegram'dan serbest kabuk komutu çalıştırılamaz.
//   - Her işlem işlem kayıtlarına "telegram" kullanıcısıyla yazılır.
package telegram

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OsmanxCan/kokpit/internal/deploy"
	"github.com/OsmanxCan/kokpit/internal/services"
	"github.com/OsmanxCan/kokpit/internal/settings"
	"github.com/OsmanxCan/kokpit/internal/store"
	"github.com/OsmanxCan/kokpit/internal/system"
	"github.com/OsmanxCan/kokpit/internal/uptime"
)

type Bot struct {
	set *settings.Store
	st  *store.Store
	sys *system.Collector
	svc *services.Manager
	up  *uptime.Engine
	dep *deploy.Engine

	API     string // test için değiştirilebilir
	mu      sync.Mutex
	pending map[string]action
	status  Status
}

// Status ayarlar sayfasında gösterilir.
type Status struct {
	Running  bool   `json:"running"`
	Bot      string `json:"bot"` // @kullanici_adi
	Error    string `json:"error,omitempty"`
	LastSeen int64  `json:"lastSeen"`
	Ignored  int    `json:"ignored"` // başka sohbetlerden gelip yok sayılan mesajlar
}

type action struct {
	kind    string // deploy | restart | ok-deploy | ok-restart
	target  string
	label   string
	expires time.Time
}

func New(set *settings.Store, st *store.Store, sys *system.Collector, svc *services.Manager, up *uptime.Engine, dep *deploy.Engine) *Bot {
	return &Bot{set: set, st: st, sys: sys, svc: svc, up: up, dep: dep, API: "https://api.telegram.org", pending: map[string]action{}}
}

func (b *Bot) Status() Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.status
}

func (b *Bot) setStatus(f func(*Status)) {
	b.mu.Lock()
	f(&b.status)
	b.mu.Unlock()
}

// ---------- Telegram API ----------

type update struct {
	UpdateID int64    `json:"update_id"`
	Message  *message `json:"message"`
	Callback *struct {
		ID      string   `json:"id"`
		Data    string   `json:"data"`
		Message *message `json:"message"`
		From    struct {
			Username  string `json:"username"`
			FirstName string `json:"first_name"`
		} `json:"from"`
	} `json:"callback_query"`
}

type message struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	From struct {
		Username  string `json:"username"`
		FirstName string `json:"first_name"`
	} `json:"from"`
}

type button struct {
	Text string `json:"text"`
	Data string `json:"callback_data"`
}

func (b *Bot) call(ctx context.Context, token, method string, params any, out any) error {
	body, _ := json.Marshal(params)
	req, _ := http.NewRequestWithContext(ctx, "POST", b.API+"/bot"+token+"/"+method, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var r struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return fmt.Errorf("Telegram yanıtı okunamadı (HTTP %d)", resp.StatusCode)
	}
	if !r.OK {
		return errors.New(r.Description)
	}
	if out != nil {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

func keyboard(rows [][]button) map[string]any {
	return map[string]any{"inline_keyboard": rows}
}

func (b *Bot) send(token string, chat int64, text string, kb map[string]any) {
	p := map[string]any{"chat_id": chat, "text": text, "parse_mode": "HTML", "disable_web_page_preview": true}
	if kb != nil {
		p["reply_markup"] = kb
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := b.call(ctx, token, "sendMessage", p, nil); err != nil {
		log.Printf("telegram: gönderilemedi: %v", err)
	}
}

func (b *Bot) edit(token string, chat, msg int64, text string, kb map[string]any) {
	p := map[string]any{"chat_id": chat, "message_id": msg, "text": text, "parse_mode": "HTML", "disable_web_page_preview": true}
	if kb != nil {
		p["reply_markup"] = kb
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	b.call(ctx, token, "editMessageText", p, nil)
}

// ---------- döngü ----------

var commands = []map[string]string{
	{"command": "durum", "description": "Sunucunun anlık durumu"},
	{"command": "servisler", "description": "PM2 uygulamaları"},
	{"command": "siteler", "description": "İzlenen sitelerin durumu"},
	{"command": "deploy", "description": "Bir projeyi deploy et"},
	{"command": "yenidenbaslat", "description": "Bir PM2 uygulamasını yeniden başlat"},
	{"command": "yardim", "description": "Komutlar"},
}

// Start komutlar açıksa Telegram'ı dinler. Ayar değişikliklerini kendisi takip eder.
func (b *Bot) Start() {
	go func() {
		var token string
		var offset int64
		for {
			s := b.set.Get()
			if !s.Telegram.Commands || s.Telegram.Token == "" || s.Telegram.ChatID == "" {
				if token != "" {
					b.setStatus(func(st *Status) { *st = Status{} })
				}
				token = ""
				time.Sleep(3 * time.Second)
				continue
			}
			if s.Telegram.Token != token {
				token, offset = s.Telegram.Token, 0
				if err := b.prepare(token); err != nil {
					b.setStatus(func(st *Status) { st.Running, st.Error = false, err.Error() })
					time.Sleep(30 * time.Second)
					token = ""
					continue
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			var ups []update
			err := b.call(ctx, token, "getUpdates", map[string]any{"offset": offset, "timeout": 25,
				"allowed_updates": []string{"message", "callback_query"}}, &ups)
			cancel()
			if err != nil {
				b.setStatus(func(st *Status) { st.Error = "Telegram'a bağlanılamadı: " + err.Error() })
				time.Sleep(10 * time.Second)
				continue
			}
			b.setStatus(func(st *Status) { st.Running, st.Error = true, "" })
			for _, u := range ups {
				offset = u.UpdateID + 1
				// ayar bu arada kapatıldıysa işleme
				if cur := b.set.Get(); !cur.Telegram.Commands || cur.Telegram.Token != token {
					break
				}
				b.handle(token, s.Telegram.ChatID, u)
			}
		}
	}()
}

// prepare bot bilgisini alır, webhook varsa kaldırır ve komut listesini ayarlar.
func (b *Bot) prepare(token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var me struct {
		Username string `json:"username"`
	}
	if err := b.call(ctx, token, "getMe", map[string]any{}, &me); err != nil {
		return fmt.Errorf("bot bilgisi alınamadı: %v", err)
	}
	b.call(ctx, token, "deleteWebhook", map[string]any{}, nil)
	b.call(ctx, token, "setMyCommands", map[string]any{"commands": commands}, nil)
	b.setStatus(func(st *Status) { st.Bot = "@" + me.Username })
	return nil
}

func (b *Bot) handle(token, chatID string, u update) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("telegram: %v", r)
		}
	}()
	var chat int64
	switch {
	case u.Message != nil:
		chat = u.Message.Chat.ID
	case u.Callback != nil && u.Callback.Message != nil:
		chat = u.Callback.Message.Chat.ID
	default:
		return
	}
	if strconv.FormatInt(chat, 10) != strings.TrimSpace(chatID) {
		b.setStatus(func(st *Status) { st.Ignored++ })
		if u.Callback != nil {
			b.call(context.Background(), token, "answerCallbackQuery", map[string]any{"callback_query_id": u.Callback.ID}, nil)
		}
		return // yetkisiz sohbet: yanıt verme
	}
	b.setStatus(func(st *Status) { st.LastSeen = time.Now().Unix() })
	if u.Callback != nil {
		b.callback(token, chat, u.Callback.Message.MessageID, u.Callback.ID, u.Callback.Data)
		return
	}
	b.command(token, chat, u.Message.Text)
}

// ---------- komutlar ----------

func (b *Bot) command(token string, chat int64, text string) {
	cmd := strings.Fields(strings.TrimSpace(text))
	if len(cmd) == 0 || !strings.HasPrefix(cmd[0], "/") {
		b.send(token, chat, "Komutları görmek için /yardim yaz.", nil)
		return
	}
	name := strings.ToLower(strings.TrimPrefix(strings.SplitN(cmd[0], "@", 2)[0], "/"))
	switch name {
	case "start", "yardim", "help":
		var sb strings.Builder
		sb.WriteString("<b>Kokpit komutları</b>\n\n")
		for _, c := range commands {
			fmt.Fprintf(&sb, "/%s — %s\n", c["command"], c["description"])
		}
		sb.WriteString("\nİşlem yapan komutlar önce onay ister.")
		b.send(token, chat, sb.String(), nil)
	case "durum":
		b.send(token, chat, b.statusText(), keyboard([][]button{{{Text: "🔄 Yenile", Data: "x:durum"}}}))
	case "servisler":
		b.send(token, chat, b.servicesText(), keyboard([][]button{{{Text: "🔄 Yenile", Data: "x:servisler"}}}))
	case "siteler":
		b.send(token, chat, b.sitesText(), keyboard([][]button{{{Text: "🔄 Yenile", Data: "x:siteler"}}}))
	case "deploy":
		b.deployMenu(token, chat, 0)
	case "yenidenbaslat", "restart":
		b.restartMenu(token, chat, 0)
	default:
		b.send(token, chat, "Bilinmeyen komut. /yardim yazarak komutları görebilirsin.", nil)
	}
}

func (b *Bot) remember(a action, ttl time.Duration) string {
	k := make([]byte, 8)
	rand.Read(k)
	key := hex.EncodeToString(k)
	a.expires = time.Now().Add(ttl)
	b.mu.Lock()
	for id, x := range b.pending { // eskileri temizle
		if time.Now().After(x.expires) {
			delete(b.pending, id)
		}
	}
	b.pending[key] = a
	b.mu.Unlock()
	return key
}

func (b *Bot) take(key string) (action, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	a, ok := b.pending[key]
	delete(b.pending, key)
	if !ok || time.Now().After(a.expires) {
		return action{}, false
	}
	return a, true
}

func (b *Bot) deployMenu(token string, chat, editMsg int64) {
	var rows [][]button
	for _, p := range b.dep.Projects() {
		if !p.Telegram {
			continue
		}
		label := "🚀 " + p.Name
		if b.dep.Busy(p.ID) != "" {
			label += " (sürüyor)"
		}
		rows = append(rows, []button{{Text: label, Data: "s:" + b.remember(action{kind: "deploy", target: p.ID, label: p.Name}, 10*time.Minute)}})
	}
	text := "<b>Hangi proje deploy edilsin?</b>"
	if len(rows) == 0 {
		text = "Telegram'dan deploy edilebilen proje yok.\nPanelde Deploy → proje ayarlarından “Telegram'dan deploy edilebilir” seçeneğini aç."
	}
	if editMsg != 0 {
		b.edit(token, chat, editMsg, text, keyboard(rows))
		return
	}
	b.send(token, chat, text, keyboard(rows))
}

func (b *Bot) restartMenu(token string, chat, editMsg int64) {
	items, err := b.svc.PM2()
	var rows [][]button
	for _, it := range items {
		icon := "🟢"
		if !it.Healthy {
			icon = "🔴"
		}
		rows = append(rows, []button{{Text: icon + " " + it.Name, Data: "s:" + b.remember(action{kind: "restart", target: it.Name, label: it.Name}, 10*time.Minute)}})
	}
	text := "<b>Hangi uygulama yeniden başlatılsın?</b>"
	if err != nil {
		text = "PM2 okunamadı: " + html.EscapeString(err.Error())
	} else if len(rows) == 0 {
		text = "PM2'de çalışan uygulama yok."
	}
	if editMsg != 0 {
		b.edit(token, chat, editMsg, text, keyboard(rows))
		return
	}
	b.send(token, chat, text, keyboard(rows))
}

func (b *Bot) callback(token string, chat, msg int64, cbID, data string) {
	answer := func(text string) {
		p := map[string]any{"callback_query_id": cbID}
		if text != "" {
			p["text"] = text
		}
		b.call(context.Background(), token, "answerCallbackQuery", p, nil)
	}
	kind, arg, _ := strings.Cut(data, ":")
	switch kind {
	case "x": // yenile
		answer("")
		var text string
		switch arg {
		case "durum":
			text = b.statusText()
		case "servisler":
			text = b.servicesText()
		case "siteler":
			text = b.sitesText()
		}
		b.edit(token, chat, msg, text, keyboard([][]button{{{Text: "🔄 Yenile", Data: "x:" + arg}}}))
	case "s": // seçim → onay iste
		a, ok := b.take(arg)
		if !ok {
			answer("Bu menünün süresi doldu, komutu tekrar yaz.")
			return
		}
		answer("")
		var q string
		switch a.kind {
		case "deploy":
			q = "🚀 <b>" + html.EscapeString(a.label) + "</b> deploy edilsin mi?"
		case "restart":
			q = "🔁 <b>" + html.EscapeString(a.label) + "</b> yeniden başlatılsın mı?"
		}
		yes := b.remember(action{kind: "ok-" + a.kind, target: a.target, label: a.label}, 2*time.Minute)
		b.edit(token, chat, msg, q+"\n\n<i>Onay 2 dakika geçerli.</i>", keyboard([][]button{{
			{Text: "✅ Evet", Data: "y:" + yes}, {Text: "Vazgeç", Data: "n:" + yes},
		}}))
	case "n":
		b.take(arg)
		answer("Vazgeçildi")
		b.edit(token, chat, msg, "Vazgeçildi.", nil)
	case "y":
		a, ok := b.take(arg)
		if !ok {
			answer("Onayın süresi doldu veya zaten kullanıldı.")
			b.edit(token, chat, msg, "Onayın süresi doldu. Komutu tekrar yaz.", nil)
			return
		}
		answer("Başlatıldı")
		switch a.kind {
		case "ok-deploy":
			b.runDeploy(token, chat, msg, a)
		case "ok-restart":
			b.edit(token, chat, msg, "🔁 <b>"+html.EscapeString(a.label)+"</b> yeniden başlatılıyor…", nil)
			_, err := b.svc.Action("pm2", a.target, "restart")
			b.st.Audit("telegram", "telegram", "pm2 restart", a.target, err == nil, errText(err))
			if err != nil {
				b.edit(token, chat, msg, "❌ <b>"+html.EscapeString(a.label)+"</b> yeniden başlatılamadı\n"+html.EscapeString(errText(err)), nil)
				return
			}
			b.edit(token, chat, msg, "✅ <b>"+html.EscapeString(a.label)+"</b> yeniden başlatıldı", nil)
		}
	default:
		answer("")
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (b *Bot) runDeploy(token string, chat, msg int64, a action) {
	p, ok := b.dep.Project(a.target)
	if !ok || !p.Telegram {
		b.edit(token, chat, msg, "Bu proje artık Telegram'dan deploy edilemiyor.", nil)
		return
	}
	run, err := b.dep.Start(p.ID, deploy.Options{Trigger: "telegram", User: "telegram"})
	b.st.Audit("telegram", "telegram", "deploy", p.Name, err == nil, errText(err))
	if err != nil {
		b.edit(token, chat, msg, "❌ "+html.EscapeString(err.Error()), nil)
		return
	}
	name := html.EscapeString(p.Name)
	b.edit(token, chat, msg, "⏳ <b>"+name+"</b> deploy ediliyor…", nil)
	// bitene kadar adımları mesajda göster
	go func() {
		last := ""
		for i := 0; ; i++ {
			time.Sleep(min(time.Duration(i+1)*time.Second, 3*time.Second))
			r, ok := b.dep.Run(run.ID)
			if !ok {
				return
			}
			if r.Status == "running" {
				if r.StepName != last && r.StepName != "" {
					last = r.StepName
					b.edit(token, chat, msg, "⏳ <b>"+name+"</b> deploy ediliyor…\nAdım: "+html.EscapeString(r.StepName), nil)
				}
				continue
			}
			dur := time.Duration(r.End-r.Start) * time.Second
			switch r.Status {
			case "ok":
				t := fmt.Sprintf("✅ <b>%s</b> deploy edildi (%s)", name, dur)
				if r.To != "" {
					t += "\n" + short(r.To) + " · " + html.EscapeString(r.Message)
				}
				b.edit(token, chat, msg, t, nil)
			case "rolled_back":
				b.edit(token, chat, msg, fmt.Sprintf("↩ <b>%s</b> deploy başarısız, önceki sürüme dönüldü\n%s", name, html.EscapeString(r.Error)), nil)
			default:
				b.edit(token, chat, msg, fmt.Sprintf("❌ <b>%s</b> deploy başarısız\n%s", name, html.EscapeString(r.Error)), nil)
			}
			return
		}
	}()
}

func short(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

// ---------- metinler ----------

func gb(v uint64) string { return fmt.Sprintf("%.1f GB", float64(v)/(1<<30)) }

func (b *Bot) statusText() string {
	s := b.sys.Latest()
	var sb strings.Builder
	fmt.Fprintf(&sb, "🖥 <b>%s</b>\n\n", html.EscapeString(s.Hostname))
	fmt.Fprintf(&sb, "CPU: <b>%%%.0f</b> · yük %.2f\n", s.CPU, s.Load1)
	fmt.Fprintf(&sb, "RAM: <b>%%%.0f</b> (%s / %s)\n", s.MemPct, gb(s.MemUsed), gb(s.MemTotal))
	fmt.Fprintf(&sb, "Disk: <b>%%%.0f</b>\n", s.RootDisk)
	up := time.Duration(s.Uptime) * time.Second
	fmt.Fprintf(&sb, "Açık kalma: %d gün %d saat\n", int(up.Hours())/24, int(up.Hours())%24)
	if items, err := b.svc.PM2(); err == nil && len(items) > 0 {
		on := 0
		for _, it := range items {
			if it.Healthy {
				on++
			}
		}
		fmt.Fprintf(&sb, "\nPM2: %d/%d çalışıyor", on, len(items))
	}
	if t, u, d := b.up.Counts(); t > 0 {
		fmt.Fprintf(&sb, "\nSiteler: %d/%d erişilebilir", u, t)
		if d > 0 {
			fmt.Fprintf(&sb, " · <b>%d çökük</b>", d)
		}
	}
	fmt.Fprintf(&sb, "\n\n<i>%s</i>", time.Now().Format("02.01.2006 15:04"))
	return sb.String()
}

func (b *Bot) servicesText() string {
	items, err := b.svc.PM2()
	if err != nil {
		return "PM2 okunamadı: " + html.EscapeString(err.Error())
	}
	if len(items) == 0 {
		return "PM2'de uygulama yok."
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	var sb strings.Builder
	sb.WriteString("<b>PM2 uygulamaları</b>\n\n")
	for _, it := range items {
		icon := "🟢"
		if !it.Healthy {
			icon = "🔴"
		}
		fmt.Fprintf(&sb, "%s <b>%s</b> · %s · %.0f MB · %d yeniden başlama\n", icon, html.EscapeString(it.Name), html.EscapeString(it.Status), float64(it.Mem)/(1<<20), it.Restart)
	}
	return sb.String()
}

func (b *Bot) sitesText() string {
	list := b.up.List()
	if len(list) == 0 {
		return "İzlenen site yok. Panelde Uptime sayfasından ekleyebilirsin."
	}
	var sb strings.Builder
	sb.WriteString("<b>Siteler</b>\n\n")
	for _, m := range list {
		icon := map[string]string{"up": "🟢", "down": "🔴", "pending": "⚪", "paused": "⏸"}[m.Status]
		fmt.Fprintf(&sb, "%s <b>%s</b>", icon, html.EscapeString(m.Name))
		if m.Uptime24 >= 0 {
			fmt.Fprintf(&sb, " · 24s %%%.1f", m.Uptime24)
		}
		if m.Status == "up" && m.LastMS > 0 {
			fmt.Fprintf(&sb, " · %d ms", m.LastMS)
		}
		if m.Status == "down" && m.LastError != "" {
			fmt.Fprintf(&sb, "\n   %s", html.EscapeString(m.LastError))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
