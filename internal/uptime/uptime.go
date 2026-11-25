// Package uptime sitelerin erişilebilirliğini düzenli aralıklarla kontrol eder.
//
// Kayıtlar uptime.json'da tutulur: son 24 saat dakika çözünürlüğünde, son 30 gün saatlik özet.
package uptime

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Monitor struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Interval  int    `json:"interval"` // saniye
	Timeout   int    `json:"timeout"`  // saniye
	Expect    int    `json:"expect"`   // beklenen HTTP kodu; 0 = 200-399 arası
	Keyword   string `json:"keyword"`  // sayfada geçmesi gereken metin (boş = kontrol yok)
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"createdAt"`
}

// Check tek bir kontrolün sonucu.
type Check struct {
	TS     int64  `json:"ts"`
	OK     bool   `json:"ok"`
	MS     int    `json:"ms"`
	Status int    `json:"status"`
	Error  string `json:"error,omitempty"`
}

// Hour saatlik özet.
type Hour struct {
	TS    int64 `json:"ts"`
	Total int   `json:"total"`
	Up    int   `json:"up"`
	SumMS int64 `json:"sumMs"`
}

type Incident struct {
	Start  int64  `json:"start"`
	End    int64  `json:"end"` // 0 = devam ediyor
	Reason string `json:"reason"`
}

type state struct {
	Checks    []Check    `json:"checks"`    // son 24 saat
	Hours     []Hour     `json:"hours"`     // son 30 gün
	Incidents []Incident `json:"incidents"` // son 50
	Up        bool       `json:"up"`
	Since     int64      `json:"since"`
	Fails     int        `json:"fails"` // art arda başarısız kontrol
	CertExp   int64      `json:"certExp"`
	CertIss   string     `json:"certIssuer"`
	LastCheck int64      `json:"lastCheck"`
	Known     bool       `json:"known"` // ilk sonuç geldi mi
}

type data struct {
	Monitors []Monitor         `json:"monitors"`
	States   map[string]*state `json:"states"`
}

// Event bildirim motoruna iletilen durum değişikliği.
type Event struct {
	Kind    string // down | up | ssl
	Monitor Monitor
	Reason  string
	Down    time.Duration // up olayında kesinti süresi
	Days    int           // ssl olayında kalan gün
}

type Engine struct {
	mu      sync.Mutex
	path    string
	d       data
	dirty   bool
	running map[string]bool
	OnEvent func(Event)
	client  *http.Client
}

const failThreshold = 2 // bu kadar art arda başarısız kontrol = çöktü

func Open(dir string) (*Engine, error) {
	e := &Engine{path: filepath.Join(dir, "uptime.json"), running: map[string]bool{}}
	b, err := os.ReadFile(e.path)
	if err == nil {
		if err := json.Unmarshal(b, &e.d); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if e.d.States == nil {
		e.d.States = map[string]*state{}
	}
	if e.d.Monitors == nil {
		e.d.Monitors = []Monitor{}
	}
	e.client = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
			DisableKeepAlives:   true,
			TLSHandshakeTimeout: 10 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("çok fazla yönlendirme")
			}
			return nil
		},
	}
	return e, nil
}

func (e *Engine) save() {
	e.mu.Lock()
	if !e.dirty {
		e.mu.Unlock()
		return
	}
	b, err := json.Marshal(e.d)
	e.dirty = false
	e.mu.Unlock()
	if err != nil {
		return
	}
	tmp := e.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, e.path)
	}
}

// Start zamanlayıcıyı başlatır: her saniye zamanı gelen izleyicileri kontrol eder.
func (e *Engine) Start() {
	go func() {
		tick := time.NewTicker(time.Second)
		lastSave := time.Now()
		for range tick.C {
			now := time.Now().Unix()
			e.mu.Lock()
			for _, m := range e.d.Monitors {
				st := e.st(m.ID)
				if !m.Enabled || e.running[m.ID] || now-st.LastCheck < int64(m.Interval) {
					continue
				}
				e.running[m.ID] = true
				go e.run(m)
			}
			e.mu.Unlock()
			if time.Since(lastSave) > time.Minute {
				e.save()
				lastSave = time.Now()
			}
		}
	}()
}

func (e *Engine) st(id string) *state {
	s, ok := e.d.States[id]
	if !ok {
		s = &state{Up: true}
		e.d.States[id] = s
	}
	return s
}

// probe tek bir HTTP kontrolü yapar.
func (e *Engine) probe(m Monitor) (Check, int64, string) {
	c := Check{TS: time.Now().Unix()}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(m.Timeout)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", m.URL, nil)
	if err != nil {
		c.Error = err.Error()
		return c, 0, ""
	}
	req.Header.Set("User-Agent", "Kokpit-Uptime/1.0 (+https://github.com)")
	req.Header.Set("Cache-Control", "no-cache")
	start := time.Now()
	resp, err := e.client.Do(req)
	c.MS = int(time.Since(start).Milliseconds())
	if err != nil {
		c.Error = shortErr(err)
		return c, 0, ""
	}
	defer resp.Body.Close()
	c.Status = resp.StatusCode
	var certExp int64
	var issuer string
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		certExp = resp.TLS.PeerCertificates[0].NotAfter.Unix()
		issuer = resp.TLS.PeerCertificates[0].Issuer.CommonName
	}
	statusOK := (m.Expect == 0 && resp.StatusCode >= 200 && resp.StatusCode < 400) || (m.Expect != 0 && resp.StatusCode == m.Expect)
	if !statusOK {
		c.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return c, certExp, issuer
	}
	if m.Keyword != "" {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		if !strings.Contains(string(body), m.Keyword) {
			c.Error = "sayfada “" + m.Keyword + "” bulunamadı"
			return c, certExp, issuer
		}
	} else {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	}
	c.OK = true
	return c, certExp, issuer
}

func shortErr(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "context deadline exceeded") || strings.Contains(s, "Client.Timeout"):
		return "zaman aşımı"
	case strings.Contains(s, "no such host"):
		return "alan adı çözülemedi (DNS)"
	case strings.Contains(s, "connection refused"):
		return "bağlantı reddedildi"
	case strings.Contains(s, "certificate"):
		return "SSL sertifika hatası"
	case strings.Contains(s, "connection reset"):
		return "bağlantı kesildi"
	}
	if i := strings.LastIndex(s, ": "); i >= 0 && len(s)-i < 80 {
		return s[i+2:]
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

func (e *Engine) run(m Monitor) {
	c, certExp, issuer := e.probe(m)
	var events []Event
	e.mu.Lock()
	delete(e.running, m.ID)
	// izleyici bu arada silinmiş olabilir
	exists := false
	for _, x := range e.d.Monitors {
		if x.ID == m.ID {
			exists = true
		}
	}
	if !exists {
		e.mu.Unlock()
		return
	}
	st := e.st(m.ID)
	st.LastCheck = c.TS
	st.Checks = append(st.Checks, c)
	cut := c.TS - 24*3600
	i := 0
	for i < len(st.Checks) && st.Checks[i].TS < cut {
		i++
	}
	if i > 0 {
		st.Checks = append([]Check(nil), st.Checks[i:]...)
	}
	hour := c.TS / 3600 * 3600
	if n := len(st.Hours); n == 0 || st.Hours[n-1].TS != hour {
		st.Hours = append(st.Hours, Hour{TS: hour})
		if len(st.Hours) > 30*24 {
			st.Hours = st.Hours[len(st.Hours)-30*24:]
		}
	}
	h := &st.Hours[len(st.Hours)-1]
	h.Total++
	if c.OK {
		h.Up++
		h.SumMS += int64(c.MS)
	}
	if certExp > 0 {
		prevDays := int((st.CertExp - time.Now().Unix()) / 86400)
		st.CertExp, st.CertIss = certExp, issuer
		days := int((certExp - time.Now().Unix()) / 86400)
		if days <= 14 && (prevDays > 14 || prevDays != days) {
			events = append(events, Event{Kind: "ssl", Monitor: m, Days: days})
		}
	}
	if c.OK {
		if !st.Up && st.Known {
			down := time.Duration(c.TS-st.Since) * time.Second
			if n := len(st.Incidents); n > 0 && st.Incidents[n-1].End == 0 {
				st.Incidents[n-1].End = c.TS
			}
			events = append(events, Event{Kind: "up", Monitor: m, Down: down})
		}
		if !st.Up || !st.Known {
			st.Since = c.TS
		}
		st.Up, st.Fails = true, 0
	} else {
		st.Fails++
		if st.Fails >= failThreshold && (st.Up || !st.Known) {
			st.Up = false
			st.Since = c.TS
			st.Incidents = append(st.Incidents, Incident{Start: c.TS, Reason: c.Error})
			if len(st.Incidents) > 50 {
				st.Incidents = st.Incidents[len(st.Incidents)-50:]
			}
			events = append(events, Event{Kind: "down", Monitor: m, Reason: c.Error})
		}
	}
	// İlk sonuç: başarılıysa ya da çöktüğü kesinleştiyse bilinir; tek hatada "bekleniyor" kalır
	if c.OK || st.Fails >= failThreshold {
		st.Known = true
	}
	e.dirty = true
	cb := e.OnEvent
	e.mu.Unlock()
	if cb != nil {
		for _, ev := range events {
			cb(ev)
		}
	}
}

// ---------- yönetim ----------

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Validate izleyiciyi doğrular ve varsayılanları doldurur.
func Validate(m *Monitor) error {
	m.URL = strings.TrimSpace(m.URL)
	if !strings.Contains(m.URL, "://") {
		m.URL = "https://" + m.URL
	}
	u, err := url.Parse(m.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("geçerli bir http:// veya https:// adresi gir")
	}
	m.Name = strings.TrimSpace(m.Name)
	if m.Name == "" {
		m.Name = u.Host
	}
	if len([]rune(m.Name)) > 60 {
		return errors.New("ad en fazla 60 karakter olabilir")
	}
	if m.Interval == 0 {
		m.Interval = 60
	}
	if m.Interval < 30 || m.Interval > 3600 {
		return errors.New("kontrol aralığı 30 saniye ile 1 saat arasında olmalı")
	}
	if m.Timeout == 0 {
		m.Timeout = 10
	}
	if m.Timeout < 2 || m.Timeout > 60 {
		return errors.New("zaman aşımı 2 ile 60 saniye arasında olmalı")
	}
	if m.Expect != 0 && (m.Expect < 100 || m.Expect > 599) {
		return errors.New("beklenen HTTP kodu 100 ile 599 arasında olmalı")
	}
	if len(m.Keyword) > 200 {
		return errors.New("aranacak metin en fazla 200 karakter olabilir")
	}
	return nil
}

func (e *Engine) Add(m Monitor) (Monitor, error) {
	if err := Validate(&m); err != nil {
		return m, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.d.Monitors) >= 50 {
		return m, errors.New("en fazla 50 izleyici eklenebilir")
	}
	m.ID, m.CreatedAt = newID(), time.Now().Unix()
	e.d.Monitors = append(e.d.Monitors, m)
	e.dirty = true
	return m, nil
}

func (e *Engine) Update(id string, m Monitor) (Monitor, error) {
	if err := Validate(&m); err != nil {
		return m, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, x := range e.d.Monitors {
		if x.ID == id {
			m.ID, m.CreatedAt = x.ID, x.CreatedAt
			if m.URL != x.URL { // adres değiştiyse sertifika bilgisini sıfırla
				st := e.st(id)
				st.CertExp, st.CertIss = 0, ""
			}
			e.d.Monitors[i] = m
			e.dirty = true
			return m, nil
		}
	}
	return m, errors.New("izleyici bulunamadı")
}

func (e *Engine) Delete(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, x := range e.d.Monitors {
		if x.ID == id {
			e.d.Monitors = append(e.d.Monitors[:i], e.d.Monitors[i+1:]...)
			delete(e.d.States, id)
			e.dirty = true
			return nil
		}
	}
	return errors.New("izleyici bulunamadı")
}

// CheckNow izleyiciyi hemen kontrol eder ve sonucu döner.
func (e *Engine) CheckNow(id string) (Check, error) {
	e.mu.Lock()
	var m *Monitor
	for i := range e.d.Monitors {
		if e.d.Monitors[i].ID == id {
			m = &e.d.Monitors[i]
		}
	}
	if m == nil {
		e.mu.Unlock()
		return Check{}, errors.New("izleyici bulunamadı")
	}
	mm := *m
	e.running[id] = true
	e.mu.Unlock()
	e.run(mm)
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.st(id)
	return st.Checks[len(st.Checks)-1], nil
}

func (e *Engine) URLs() map[string]bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	m := map[string]bool{}
	for _, x := range e.d.Monitors {
		m[x.URL] = true
	}
	return m
}

// ---------- özetler ----------

type Summary struct {
	Monitor
	Status    string  `json:"status"` // up | down | pending | paused
	Since     int64   `json:"since"`
	Uptime24  float64 `json:"uptime24"`
	Uptime7   float64 `json:"uptime7"`
	Uptime30  float64 `json:"uptime30"`
	AvgMS     int     `json:"avgMs"`
	LastMS    int     `json:"lastMs"`
	LastError string  `json:"lastError"`
	LastCheck int64   `json:"lastCheck"`
	CertExp   int64   `json:"certExp"`
	CertIss   string  `json:"certIssuer"`
	Bars      []Bar   `json:"bars"` // son 24 saat, saatlik
}

// Bar zaman çizelgesindeki bir dilim.
type Bar struct {
	TS     int64   `json:"ts"`
	Uptime float64 `json:"uptime"` // -1 = veri yok
	AvgMS  int     `json:"avgMs"`
}

func ratio(hours []Hour, since int64) (float64, int) {
	var total, up int
	var sum int64
	for _, h := range hours {
		if h.TS >= since {
			total += h.Total
			up += h.Up
			sum += h.SumMS
		}
	}
	if total == 0 {
		return -1, 0
	}
	avg := 0
	if up > 0 {
		avg = int(sum / int64(up))
	}
	return float64(up) / float64(total) * 100, avg
}

func (e *Engine) summary(m Monitor) Summary {
	st := e.st(m.ID)
	now := time.Now().Unix()
	s := Summary{Monitor: m, Since: st.Since, LastCheck: st.LastCheck, CertExp: st.CertExp, CertIss: st.CertIss}
	switch {
	case !m.Enabled:
		s.Status = "paused"
	case !st.Known:
		s.Status = "pending"
	case st.Up:
		s.Status = "up"
	default:
		s.Status = "down"
	}
	s.Uptime24, s.AvgMS = ratio(st.Hours, now-24*3600)
	s.Uptime7, _ = ratio(st.Hours, now-7*86400)
	s.Uptime30, _ = ratio(st.Hours, now-30*86400)
	if n := len(st.Checks); n > 0 {
		s.LastMS = st.Checks[n-1].MS
		s.LastError = st.Checks[n-1].Error
	}
	// son 24 saat için saatlik çubuklar (en eski → en yeni)
	byHour := map[int64]Hour{}
	for _, h := range st.Hours {
		byHour[h.TS] = h
	}
	cur := now / 3600 * 3600
	for i := 23; i >= 0; i-- {
		ts := cur - int64(i)*3600
		b := Bar{TS: ts, Uptime: -1}
		if h, ok := byHour[ts]; ok && h.Total > 0 {
			b.Uptime = float64(h.Up) / float64(h.Total) * 100
			if h.Up > 0 {
				b.AvgMS = int(h.SumMS / int64(h.Up))
			}
		}
		s.Bars = append(s.Bars, b)
	}
	return s
}

func (e *Engine) List() []Summary {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []Summary{}
	for _, m := range e.d.Monitors {
		out = append(out, e.summary(m))
	}
	return out
}

type Detail struct {
	Summary
	Checks    []Check    `json:"checks"`
	Hours     []Hour     `json:"hours"`
	Incidents []Incident `json:"incidents"`
}

func (e *Engine) Detail(id string) (Detail, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, m := range e.d.Monitors {
		if m.ID == id {
			st := e.st(id)
			d := Detail{Summary: e.summary(m), Checks: append([]Check{}, st.Checks...), Hours: append([]Hour{}, st.Hours...),
				Incidents: append([]Incident{}, st.Incidents...)}
			sort.Slice(d.Incidents, func(i, j int) bool { return d.Incidents[i].Start > d.Incidents[j].Start })
			return d, nil
		}
	}
	return Detail{}, errors.New("izleyici bulunamadı")
}

// Counts genel bakış için: toplam, çalışan, çöken.
func (e *Engine) Counts() (total, up, down int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, m := range e.d.Monitors {
		if !m.Enabled {
			continue
		}
		total++
		st := e.st(m.ID)
		if !st.Known {
			continue
		}
		if st.Up {
			up++
		} else {
			down++
		}
	}
	return
}

// Flush kapanırken veriyi diske yazar.
func (e *Engine) Flush() { e.save() }
