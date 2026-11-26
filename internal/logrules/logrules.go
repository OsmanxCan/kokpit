// Package logrules uygulama loglarını izler ve tanımlı kalıba uyan satırlarda geçen
// IP adreslerini, eşik aşılınca engeller (fail2ban benzeri, ama panelden ayarlanır).
//
// Örnek: Accounts başarısız girişleri "login failed ip=<IP>" diye logluyorsa, bu satır
// 5 dakikada 10 kez geçen IP 1 saat engellenir.
package logrules

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/OsmanxCan/kokpit/internal/settings"
)

const ipPart = `(?P<ip>[0-9]{1,3}(?:\.[0-9]{1,3}){3}|[0-9A-Fa-f:]*:[0-9A-Fa-f:.]+)`

// Compile kuralın kalıbını düzenli ifadeye çevirir. Düz kalıpta <IP> IP adresini,
// * herhangi bir metni temsil eder; geri kalanı harfiyen aranır.
func Compile(r settings.LogRule) (*regexp.Regexp, error) {
	p := strings.TrimSpace(r.Pattern)
	if p == "" {
		return nil, errors.New("kalıp boş")
	}
	if !strings.Contains(p, "<IP>") {
		return nil, errors.New("kalıpta IP'nin yerini gösteren <IP> olmalı")
	}
	if strings.Count(p, "<IP>") > 1 {
		return nil, errors.New("kalıpta sadece bir <IP> olabilir")
	}
	var expr string
	if r.Regex {
		expr = strings.Replace(p, "<IP>", ipPart, 1)
	} else {
		parts := strings.Split(p, "<IP>")
		q := func(s string) string {
			segs := strings.Split(s, "*")
			for i := range segs {
				segs[i] = regexp.QuoteMeta(segs[i])
			}
			return strings.Join(segs, ".*?")
		}
		expr = q(parts[0]) + ipPart + q(parts[1])
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("kalıp geçersiz: %v", err)
	}
	return re, nil
}

var unitRe = regexp.MustCompile(`^[A-Za-z0-9@._:-]{1,100}$`)

// Validate kuralı doğrular ve varsayılanları doldurur.
func Validate(r *settings.LogRule) error {
	r.Name = strings.TrimSpace(r.Name)
	r.Path = strings.TrimSpace(r.Path)
	if r.Name == "" || len([]rune(r.Name)) > 50 {
		return errors.New("kural adı 1–50 karakter olmalı")
	}
	switch r.Source {
	case "file":
		if !filepath.IsAbs(r.Path) {
			return errors.New("log dosyası tam yol olmalı (örn. /var/log/uygulama/error.log)")
		}
		r.Path = filepath.Clean(r.Path)
		if strings.HasPrefix(r.Path, "/proc") || strings.HasPrefix(r.Path, "/sys") || strings.HasPrefix(r.Path, "/dev") {
			return errors.New("bu konum log dosyası olamaz")
		}
	case "journal":
		if !unitRe.MatchString(r.Path) || strings.HasPrefix(r.Path, "-") {
			return errors.New("servis adı geçersiz (örn. accounts.service)")
		}
	case "pm2":
		if !unitRe.MatchString(r.Path) || strings.HasPrefix(r.Path, "-") {
			return errors.New("PM2 uygulama adı geçersiz")
		}
	default:
		return errors.New("kaynak dosya, journal veya pm2 olmalı")
	}
	if _, err := Compile(*r); err != nil {
		return err
	}
	if r.Threshold == 0 {
		r.Threshold = 10
	}
	if r.Window == 0 {
		r.Window = 5
	}
	if r.Threshold < 1 || r.Threshold > 10000 {
		return errors.New("eşik 1 ile 10000 arasında olmalı")
	}
	if r.Window < 1 || r.Window > 1440 {
		return errors.New("süre penceresi 1 dakika ile 1 gün arasında olmalı")
	}
	if r.BanHours < 0 || r.BanHours > 24*365 {
		return errors.New("engelleme süresi 0 (kalıcı) ile 8760 saat arasında olmalı")
	}
	switch r.Action {
	case "", "auto":
		r.Action = "auto"
	case "cloudflare", "firewall", "notify":
	default:
		return errors.New("işlem auto, cloudflare, firewall veya notify olmalı")
	}
	return nil
}

// ---------- deneme ----------

type TestResult struct {
	Lines     int            `json:"lines"`     // okunan satır
	Matches   int            `json:"matches"`   // eşleşen satır
	IPs       map[string]int `json:"ips"`       // IP → eşleşme (en çok 20)
	Samples   []string       `json:"samples"`   // örnek eşleşen satırlar
	Unmatched []string       `json:"unmatched"` // eşleşme yoksa son satırlardan örnek (kalıbı yazmaya yardım)
	WouldBan  int            `json:"wouldBan"`  // toplamda eşiği aşan IP sayısı (yaklaşık)
}

// Test kuralı kaynağın son satırlarında dener.
func Test(r settings.LogRule, pm2Logs func(string) []string) (TestResult, error) {
	re, err := Compile(r)
	if err != nil {
		return TestResult{}, err
	}
	lines, err := lastLines(r, 5000, pm2Logs)
	if err != nil {
		return TestResult{}, err
	}
	res := TestResult{Lines: len(lines), IPs: map[string]int{}}
	for _, l := range lines {
		ip := match(re, l)
		if ip == "" {
			continue
		}
		res.Matches++
		res.IPs[ip]++
		if len(res.Samples) < 5 {
			res.Samples = append(res.Samples, trunc(l, 300))
		}
	}
	for _, n := range res.IPs {
		if n >= r.Threshold {
			res.WouldBan++
		}
	}
	if len(res.IPs) > 20 {
		type kv struct {
			ip string
			n  int
		}
		var l []kv
		for k, v := range res.IPs {
			l = append(l, kv{k, v})
		}
		sort.Slice(l, func(i, j int) bool { return l[i].n > l[j].n })
		res.IPs = map[string]int{}
		for _, x := range l[:20] {
			res.IPs[x.ip] = x.n
		}
	}
	if res.Matches == 0 {
		for i := len(lines) - 1; i >= 0 && len(res.Unmatched) < 5; i-- {
			if strings.TrimSpace(lines[i]) != "" {
				res.Unmatched = append(res.Unmatched, trunc(lines[i], 300))
			}
		}
	}
	return res, nil
}

func match(re *regexp.Regexp, l string) string {
	m := re.FindStringSubmatch(l)
	if m == nil {
		return ""
	}
	ip := m[re.SubexpIndex("ip")]
	if net.ParseIP(ip) == nil {
		return ""
	}
	return ip
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func lastLines(r settings.LogRule, n int, pm2Logs func(string) []string) ([]string, error) {
	switch r.Source {
	case "file":
		f, err := os.Open(r.Path)
		if err != nil {
			return nil, fmt.Errorf("dosya açılamadı: %v", err)
		}
		defer f.Close()
		if fi, err := f.Stat(); err == nil && fi.Size() > 8<<20 {
			f.Seek(-8<<20, 2)
		}
		var lines []string
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			lines = append(lines, sc.Text())
			if len(lines) > 2*n {
				lines = lines[n:]
			}
		}
		if len(lines) > n {
			lines = lines[len(lines)-n:]
		}
		return lines, nil
	case "journal":
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "journalctl", "-u", r.Path, "-n", fmt.Sprint(n), "-o", "cat", "--no-pager").Output()
		if err != nil {
			return nil, fmt.Errorf("journalctl: %v", err)
		}
		return strings.Split(string(out), "\n"), nil
	case "pm2":
		files := pm2Logs(r.Path)
		if len(files) == 0 {
			return nil, errors.New("PM2 uygulamasının log dosyası bulunamadı")
		}
		var all []string
		for _, f := range files {
			rr := r
			rr.Source, rr.Path = "file", f
			l, err := lastLines(rr, n, nil)
			if err == nil {
				all = append(all, l...)
			}
		}
		return all, nil
	}
	return nil, errors.New("bilinmeyen kaynak")
}

// ---------- canlı izleme ----------

type BanFunc func(ip, reason string, hours int, action string) (string, error) // backend adı döner

type Hit struct {
	Rule    string `json:"rule"`
	IP      string `json:"ip"`
	TS      int64  `json:"ts"`
	Count   int    `json:"count"`
	Result  string `json:"result"` // "cloudflare'de engellendi" / hata
	Blocked bool   `json:"blocked"`
}

type Engine struct {
	mu      sync.Mutex
	rules   func() []settings.LogRule
	Ban     BanFunc
	OnHit   func(settings.LogRule, Hit)
	PM2Logs func(string) []string
	running map[string]context.CancelFunc
	sigs    map[string]string // kural id → çalışan kuralın imzası (değişince yeniden başlat)
	counts  map[string]map[string][]time.Time
	hits    []Hit
	status  map[string]string // kural id → durum/hata
}

func New(rules func() []settings.LogRule) *Engine {
	return &Engine{rules: rules, running: map[string]context.CancelFunc{}, sigs: map[string]string{},
		counts: map[string]map[string][]time.Time{}, status: map[string]string{}}
}

func sig(r settings.LogRule) string {
	return fmt.Sprintf("%s|%s|%s|%v|%v", r.Source, r.Path, r.Pattern, r.Regex, r.Enabled)
}

// Sync çalışan izleyicileri ayarlarla eşitler (eklenen/silinen/değişen kurallar).
func (e *Engine) Sync() {
	want := map[string]settings.LogRule{}
	for _, r := range e.rules() {
		if r.Enabled {
			want[r.ID] = r
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, cancel := range e.running {
		r, ok := want[id]
		if !ok || sig(r) != e.sigs[id] {
			cancel()
			delete(e.running, id)
			delete(e.sigs, id)
			delete(e.counts, id)
			if !ok {
				delete(e.status, id)
			}
		}
	}
	for id, r := range want {
		if _, ok := e.running[id]; ok {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		e.running[id] = cancel
		e.sigs[id] = sig(r)
		e.counts[id] = map[string][]time.Time{}
		e.status[id] = "başlatılıyor"
		go e.follow(ctx, r)
	}
}

// Start ayar değişikliklerini düzenli olarak uygular.
func (e *Engine) Start() {
	go func() {
		for {
			e.Sync()
			time.Sleep(10 * time.Second)
		}
	}()
}

func (e *Engine) setStatus(id, s string) {
	e.mu.Lock()
	if _, ok := e.running[id]; ok {
		e.status[id] = s
	}
	e.mu.Unlock()
}

func (e *Engine) follow(ctx context.Context, r settings.LogRule) {
	re, err := Compile(r)
	if err != nil {
		e.setStatus(r.ID, err.Error())
		return
	}
	for ctx.Err() == nil {
		var cmd *exec.Cmd
		switch r.Source {
		case "file":
			cmd = exec.CommandContext(ctx, "tail", "-n", "0", "-F", r.Path)
		case "journal":
			cmd = exec.CommandContext(ctx, "journalctl", "-u", r.Path, "-f", "-n", "0", "-o", "cat", "--no-pager")
		case "pm2":
			files := []string{}
			if e.PM2Logs != nil {
				files = e.PM2Logs(r.Path)
			}
			if len(files) == 0 {
				e.setStatus(r.ID, "PM2 uygulamasının log dosyası bulunamadı")
				sleep(ctx, time.Minute)
				continue
			}
			cmd = exec.CommandContext(ctx, "tail", append([]string{"-q", "-n", "0", "-F"}, files...)...)
		}
		out, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err != nil {
			e.setStatus(r.ID, "izlenemiyor: "+err.Error())
			sleep(ctx, 30*time.Second)
			continue
		}
		e.setStatus(r.ID, "izleniyor")
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			if ip := match(re, sc.Text()); ip != "" {
				e.Feed(r, ip, time.Now())
			}
		}
		cmd.Wait()
		if ctx.Err() == nil {
			e.setStatus(r.ID, "kaynak kapandı, yeniden deneniyor")
			sleep(ctx, 10*time.Second)
		}
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// Feed bir eşleşmeyi sayar; eşik aşılırsa engeller.
func (e *Engine) Feed(r settings.LogRule, ip string, now time.Time) {
	window := time.Duration(r.Window) * time.Minute
	e.mu.Lock()
	c := e.counts[r.ID]
	if c == nil {
		c = map[string][]time.Time{}
		e.counts[r.ID] = c
	}
	l := append(c[ip], now)
	i := 0
	for i < len(l) && now.Sub(l[i]) > window {
		i++
	}
	l = l[i:]
	if len(l) < r.Threshold {
		c[ip] = l
		if len(c) > 50000 { // bellek sınırı
			e.counts[r.ID] = map[string][]time.Time{}
		}
		e.mu.Unlock()
		return
	}
	delete(c, ip) // eşik aşıldı; sayacı sıfırla
	e.mu.Unlock()

	h := Hit{Rule: r.Name, IP: ip, TS: now.Unix(), Count: len(l)}
	if r.Action == "notify" || e.Ban == nil {
		h.Result = "sadece bildirildi"
	} else if backend, err := e.Ban(ip, r.Name, r.BanHours, r.Action); err != nil {
		h.Result = err.Error()
	} else {
		h.Blocked, h.Result = true, backend+" ile engellendi"
	}
	e.mu.Lock()
	e.hits = append(e.hits, h)
	if len(e.hits) > 200 {
		e.hits = e.hits[len(e.hits)-200:]
	}
	e.mu.Unlock()
	if e.OnHit != nil {
		e.OnHit(r, h)
	}
}

func (e *Engine) Hits() []Hit {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Hit, len(e.hits))
	for i := range e.hits {
		out[len(e.hits)-1-i] = e.hits[i]
	}
	return out
}

func (e *Engine) Status() map[string]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := map[string]string{}
	for k, v := range e.status {
		out[k] = v
	}
	return out
}
