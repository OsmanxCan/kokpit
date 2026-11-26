package server

import (
	"net"
	"os"

	"encoding/json"
	"github.com/OsmanxCan/kokpit/internal/files"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/OsmanxCan/kokpit/internal/alerts"
	"github.com/OsmanxCan/kokpit/internal/crypt"
	"github.com/OsmanxCan/kokpit/internal/execx"
	"github.com/OsmanxCan/kokpit/internal/settings"
	"github.com/OsmanxCan/kokpit/internal/store"
)

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(v); err != nil {
		writeErr(w, 400, "Geçersiz istek: "+err.Error())
		return false
	}
	return true
}

// ---------- ayarlar ----------

type settingsView struct {
	settings.Settings
	TokenSet    bool   `json:"tokenSet"`
	TokenMasked string `json:"tokenMasked"`
	CFSet       bool   `json:"cfSet"`
	CFMasked    string `json:"cfMasked"`
}

func (s *Server) settingsView() settingsView {
	cur := s.set.Get()
	v := settingsView{Settings: cur, TokenSet: cur.Telegram.Token != "", TokenMasked: settings.MaskToken(cur.Telegram.Token)}
	v.Telegram.Token = "" // token asla tarayıcıya gönderilmez
	v.CFSet, v.CFMasked = cur.CFToken != "", settings.MaskToken(cur.CFToken)
	v.CFToken = ""
	return v
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.settingsView())
}

var chatIDRe = regexp.MustCompile(`^(-?\d{1,20}|@[A-Za-z0-9_]{4,64})$`)
var tokenRe = regexp.MustCompile(`^\d{5,15}:[A-Za-z0-9_-]{30,64}$`)

// settingsPatch: gönderilmeyen alanlar değişmez.
type settingsPatch struct {
	Telegram *struct {
		Token      *string `json:"token"`
		ClearToken bool    `json:"clearToken"`
		ChatID     *string `json:"chatId"`
		Prefix     *bool   `json:"prefix"`
		Commands   *bool   `json:"commands"`
	} `json:"telegram"`
	Thresholds    *settings.Thresholds      `json:"thresholds"`
	Events        map[string]settings.Event `json:"events"`
	WatchServices *[]string                 `json:"watchServices"`
	SessionIPBind *bool                     `json:"sessionIpBind"`
	UpdateCheck   *bool                     `json:"updateCheck"`
	FileRoots     *[]files.Root             `json:"fileRoots"`
	TerminalOff   *bool                     `json:"terminalOff"`
	PortScan      *settings.PortScan        `json:"portScan"`
}

func (s *Server) handleSettingsPut(w http.ResponseWriter, r *http.Request) {
	var p settingsPatch
	if !readJSON(w, r, &p) {
		return
	}
	var changed []string
	// doğrulama
	if p.Telegram != nil {
		if p.Telegram.Token != nil {
			t := strings.TrimSpace(*p.Telegram.Token)
			t = strings.TrimPrefix(t, "bot")
			if t != "" && !tokenRe.MatchString(t) {
				writeErr(w, 400, "Token biçimi hatalı. Doğru biçim: 7123456789:AAH… (rakamlar, iki nokta, harfler)")
				return
			}
			*p.Telegram.Token = t
		}
		if p.Telegram.ChatID != nil {
			c := strings.TrimSpace(*p.Telegram.ChatID)
			if c != "" && !chatIDRe.MatchString(c) {
				writeErr(w, 400, "Chat id sadece rakamlardan oluşmalı (grup için başında - olabilir)")
				return
			}
			*p.Telegram.ChatID = c
		}
	}
	if t := p.Thresholds; t != nil {
		if t.CPU < 10 || t.CPU > 100 || t.RAM < 10 || t.RAM > 100 || t.Disk < 10 || t.Disk > 100 {
			writeErr(w, 400, "Yüzde eşikleri 10 ile 100 arasında olmalı")
			return
		}
		if t.CPUMinutes < 1 || t.CPUMinutes > 120 || t.RAMMinutes < 1 || t.RAMMinutes > 120 {
			writeErr(w, 400, "Süreler 1 ile 120 dakika arasında olmalı")
			return
		}
		if t.SSHBrute < 1 || t.SSHBrute > 100000 {
			writeErr(w, 400, "SSH deneme eşiği 1 ile 100000 arasında olmalı")
			return
		}
	}
	for id, ev := range p.Events {
		if _, ok := settings.EventByID(id); !ok {
			writeErr(w, 400, "Bilinmeyen olay: "+id)
			return
		}
		if ev.Cooldown < 0 || ev.Cooldown > 10080 {
			writeErr(w, 400, "Bekleme süresi 0 ile 10080 dakika (1 hafta) arasında olmalı")
			return
		}
	}
	if p.WatchServices != nil {
		for _, n := range *p.WatchServices {
			if !execx.SafeName(n) {
				writeErr(w, 400, "Geçersiz servis adı: "+n)
				return
			}
		}
	}

	if p.FileRoots != nil {
		if len(*p.FileRoots) > 20 {
			writeErr(w, 400, "En fazla 20 klasör eklenebilir")
			return
		}
		seen := map[string]bool{}
		for i := range *p.FileRoots {
			fr := &(*p.FileRoots)[i]
			if err := files.ValidateRoot(fr); err != nil {
				writeErr(w, 400, err.Error())
				return
			}
			if seen[fr.Name] {
				writeErr(w, 400, "Aynı adda iki klasör olamaz: "+fr.Name)
				return
			}
			seen[fr.Name] = true
			if fi, err := os.Stat(fr.Path); err != nil || !fi.IsDir() {
				if fr.Path == settings.DefaultStorage && os.IsNotExist(err) {
					os.MkdirAll(fr.Path, 0o750)
					continue
				}
				writeErr(w, 400, fr.Path+" bulunamadı veya bir klasör değil")
				return
			}
		}
	}

	if ps := p.PortScan; ps != nil {
		switch {
		case ps.Threshold < 5 || ps.Threshold > 1000:
			writeErr(w, 400, "Port eşiği 5 ile 1000 arasında olmalı")
			return
		case ps.Window < 10 || ps.Window > 3600:
			writeErr(w, 400, "Süre penceresi 10 saniye ile 1 saat arasında olmalı")
			return
		case ps.BanHours < 0 || ps.BanHours > 24*365:
			writeErr(w, 400, "Engelleme süresi 0 (kalıcı) ile 8760 saat arasında olmalı")
			return
		case len(ps.Whitelist) > 200:
			writeErr(w, 400, "Beyaz liste en fazla 200 kayıt olabilir")
			return
		}
		wl := []string{}
		for _, x := range ps.Whitelist {
			x = strings.TrimSpace(x)
			if x == "" {
				continue
			}
			if net.ParseIP(x) == nil {
				if _, _, err := net.ParseCIDR(x); err != nil {
					writeErr(w, 400, "Beyaz listede geçersiz kayıt: "+x)
					return
				}
			}
			wl = append(wl, x)
		}
		ps.Whitelist = wl
	}

	err := s.set.Update(func(c *settings.Settings) {
		if t := p.Telegram; t != nil {
			if t.ClearToken {
				c.Telegram.Token = ""
				changed = append(changed, "telegram token silindi")
			} else if t.Token != nil && *t.Token != "" {
				c.Telegram.Token = *t.Token
				changed = append(changed, "telegram token")
			}
			if t.ChatID != nil {
				c.Telegram.ChatID = *t.ChatID
				changed = append(changed, "telegram chat id")
			}
			if t.Prefix != nil {
				c.Telegram.Prefix = *t.Prefix
			}
			if t.Commands != nil && *t.Commands != c.Telegram.Commands {
				c.Telegram.Commands = *t.Commands
				changed = append(changed, map[bool]string{true: "telegram komutları açıldı", false: "telegram komutları kapatıldı"}[*t.Commands])
			}
		}
		if p.Thresholds != nil {
			c.Thresholds = *p.Thresholds
			changed = append(changed, "eşikler")
		}
		for id, ev := range p.Events {
			ev.Template = settings.CleanTemplate(ev.Template)
			if def, _ := settings.EventByID(id); ev.Template == strings.TrimSpace(def.Template) {
				ev.Template = "" // varsayılanla aynıysa boş bırak (ileride varsayılan güncellenirse gelsin)
			}
			c.Events[id] = ev
			changed = append(changed, "olay:"+id)
		}
		if p.WatchServices != nil {
			c.WatchServices = *p.WatchServices
			changed = append(changed, "izlenen servisler")
		}
		if p.SessionIPBind != nil {
			c.SessionIPBind = *p.SessionIPBind
			changed = append(changed, "oturum IP bağlama")
		}
		if p.UpdateCheck != nil {
			c.UpdateCheck = *p.UpdateCheck
			changed = append(changed, "güncelleme kontrolü")
		}
		if p.FileRoots != nil {
			c.FileRoots = *p.FileRoots
			var ns []string
			for _, r := range c.FileRoots {
				ns = append(ns, r.Path)
			}
			changed = append(changed, "dosya klasörleri ("+strings.Join(ns, ", ")+")")
		}
		if p.PortScan != nil {
			c.PortScan = *p.PortScan
			changed = append(changed, "port tarama ayarları")
		}
		if p.TerminalOff != nil {
			c.TerminalOff = *p.TerminalOff
			if c.TerminalOff {
				changed = append(changed, "terminal kapatıldı")
			} else {
				changed = append(changed, "terminal açıldı")
			}
		}
	})
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "ayar", strings.Join(changed, ", "), err == nil, errStr(err))
	if err != nil {
		writeErr(w, 500, "Ayarlar kaydedilemedi: "+err.Error())
		return
	}
	if p.UpdateCheck != nil {
		s.upd.SetEnabled(*p.UpdateCheck)
	}
	writeJSON(w, s.settingsView())
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"events": settings.Events, "common": settings.CommonVars})
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Event    string `json:"event"`
		Template string `json:"template"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	html, err := s.alerts.Preview(req.Event, settings.CleanTemplate(req.Template))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"html": html})
}

// handleTelegramTest: formdaki (henüz kaydedilmemiş) değerlerle dener; boş bırakılan alan için kayıtlı değer kullanılır.
func (s *Server) handleTelegramTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token  string `json:"token"`
		ChatID string `json:"chatId"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	cur := s.set.Get().Telegram
	token := strings.TrimPrefix(strings.TrimSpace(req.Token), "bot")
	if token == "" {
		token = cur.Token
	}
	chat := strings.TrimSpace(req.ChatID)
	if chat == "" {
		chat = cur.ChatID
	}
	host := s.sys.Latest().Hostname
	err := alerts.SendTelegram(token, chat, "✅ <b>Kokpit bağlantısı çalışıyor</b>\n"+settings.Render("{{sunucu}} · {{tarih}} {{saat}}", nil, host))
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "telegram test", chat, err == nil, errStr(err))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleEventTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Template string `json:"template"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.alerts.SendTest(r.PathValue("id"), settings.CleanTemplate(req.Template)); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleAllServices: izlenecek servis seçimi için tüm systemd servisleri.
func (s *Server) handleAllServices(w http.ResponseWriter, r *http.Request) {
	items, err := s.svc.Systemd(true)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	type row struct {
		Name    string `json:"name"`
		Desc    string `json:"desc"`
		Status  string `json:"status"`
		Healthy bool   `json:"healthy"`
		Watched bool   `json:"watched"`
	}
	watched := s.set.Watched()
	out := []row{}
	seen := map[string]bool{}
	for _, it := range items {
		seen[it.Name] = true
		out = append(out, row{it.Name, it.Info, it.Status, it.Healthy, watched[it.Name]})
	}
	// ayarda olup sunucuda bulunmayanlar da görünsün
	for n := range watched {
		if !seen[n] {
			out = append(out, row{n, "sunucuda bulunamadı", "yok", false, true})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Watched != out[j].Watched {
			return out[i].Watched
		}
		if (out[i].Status == "active/running") != (out[j].Status == "active/running") {
			return out[i].Status == "active/running"
		}
		return out[i].Name < out[j].Name
	})
	writeJSON(w, out)
}

// ---------- kullanıcılar ve hesap ----------

// Kurulumu tamamlanmamış 2FA sırları (oturum + kullanıcı adı başına, 15 dk geçerli)
var pending2FA = struct {
	sync.Mutex
	m map[string]pendingSecret
}{m: map[string]pendingSecret{}}

type pendingSecret struct {
	secret string
	exp    time.Time
}

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{3,32}$`)

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	type row struct {
		Username  string `json:"username"`
		CreatedAt int64  `json:"createdAt"`
		UpdatedAt int64  `json:"updatedAt"`
	}
	out := []row{}
	for _, u := range s.st.Users() {
		out = append(out, row{u.Username, u.CreatedAt, u.UpdatedAt})
	}
	writeJSON(w, map[string]any{"users": out, "me": sess(r).Username})
}

// checkCurrent hassas işlemlerden önce oturumdaki kullanıcının şifresini doğrular.
func (s *Server) checkCurrent(w http.ResponseWriter, r *http.Request, pw string) bool {
	ip := clientIP(r)
	if s.st.FailCount(ip, store.Now()-failWindow) >= maxFails {
		writeErr(w, 429, "Çok fazla hatalı deneme. 15 dakika sonra tekrar dene.")
		return false
	}
	u, ok := s.st.User(sess(r).Username)
	if !ok || !crypt.CheckPassword(u.PassHash, pw) {
		s.st.AddFail(ip)
		time.Sleep(500 * time.Millisecond)
		writeErr(w, 403, "Mevcut şifren hatalı")
		return false
	}
	return true
}

// handleNew2FA yeni bir 2FA sırrı üretir; kullanıcı kodu doğrulayana kadar kaydedilmez.
func (s *Server) handleNew2FA(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !usernameRe.MatchString(req.Username) {
		writeErr(w, 400, "Kullanıcı adı 3–32 karakter olmalı; harf, rakam, nokta, tire ve alt çizgi kullanılabilir")
		return
	}
	secret := crypt.NewTOTPSecret()
	key := sess(r).TokenH + ":" + req.Username
	pending2FA.Lock()
	for k, p := range pending2FA.m {
		if time.Now().After(p.exp) {
			delete(pending2FA.m, k)
		}
	}
	pending2FA.m[key] = pendingSecret{secret, time.Now().Add(15 * time.Minute)}
	pending2FA.Unlock()
	writeJSON(w, map[string]any{"secret": secret, "url": crypt.TOTPURL("Kokpit", req.Username, secret)})
}

// handleUserSave yeni kullanıcı ekler veya bir kullanıcının 2FA'sını (ve isteğe bağlı şifresini) yeniler.
func (s *Server) handleUserSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username        string `json:"username"`
		Password        string `json:"password"`
		Code            string `json:"code"`
		CurrentPassword string `json:"currentPassword"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !s.checkCurrent(w, r, req.CurrentPassword) {
		return
	}
	si := sess(r)
	key := si.TokenH + ":" + req.Username
	pending2FA.Lock()
	p, ok := pending2FA.m[key]
	pending2FA.Unlock()
	if !ok || time.Now().After(p.exp) {
		writeErr(w, 400, "2FA kurulumu bulunamadı veya süresi doldu; QR kodu yeniden oluştur")
		return
	}
	if _, err := crypt.ValidateTOTP(p.secret, req.Code); err != nil {
		writeErr(w, 400, "Doğrulama kodu hatalı. Telefonundaki saatin doğru olduğundan emin ol.")
		return
	}
	existing, exists := s.st.User(req.Username)
	hash := existing.PassHash
	if req.Password != "" || !exists {
		if len(req.Password) < 12 {
			writeErr(w, 400, "Şifre en az 12 karakter olmalı")
			return
		}
		h, err := crypt.HashPassword(req.Password)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		hash = h
	}
	if err := s.st.UpsertUser(store.User{Username: req.Username, PassHash: hash, TOTPSecret: p.secret}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	pending2FA.Lock()
	delete(pending2FA.m, key)
	pending2FA.Unlock()
	action := map[bool]string{true: "kullanici 2fa yenilendi", false: "kullanici eklendi"}[exists]
	s.st.Audit(si.Username, clientIP(r), action, req.Username, true, "")
	if req.Username == si.Username { // kendi hesabıysa eski oturumlar düştü; bu tarayıcıda devam etsin
		s.issueSession(w, r, si.Username)
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"currentPassword"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	si := sess(r)
	if name == si.Username {
		writeErr(w, 400, "Kendi hesabını silemezsin")
		return
	}
	if !s.checkCurrent(w, r, req.CurrentPassword) {
		return
	}
	if _, ok := s.st.User(name); !ok {
		writeErr(w, 404, "Kullanıcı bulunamadı")
		return
	}
	err := s.st.DeleteUser(name)
	s.st.Audit(si.Username, clientIP(r), "kullanici silindi", name, err == nil, errStr(err))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handlePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !s.checkCurrent(w, r, req.Current) {
		return
	}
	if len(req.New) < 12 {
		writeErr(w, 400, "Yeni şifre en az 12 karakter olmalı")
		return
	}
	if req.New == req.Current {
		writeErr(w, 400, "Yeni şifre eskisiyle aynı olamaz")
		return
	}
	si := sess(r)
	u, _ := s.st.User(si.Username)
	h, err := crypt.HashPassword(req.New)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	u.PassHash = h
	if err := s.st.UpsertUser(u); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.st.Audit(si.Username, clientIP(r), "sifre degistirildi", si.Username, true, "")
	s.issueSession(w, r, si.Username)
	writeJSON(w, map[string]any{"ok": true})
}
