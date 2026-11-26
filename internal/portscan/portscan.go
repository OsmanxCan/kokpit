// Package portscan sunucuya yapılan port taramalarını (Nmap, masscan vb.) tespit eder.
//
// Firewall'un (UFW) engellediği paketler kernel loguna yazılır. Aynı IP kısa sürede çok
// sayıda farklı porta dokunursa bu bir taramadır. UFW'nin kendi logları dakikada birkaç
// satırla sınırlı olduğundan Kokpit, engellenen paketler için IP başına sınırlı ayrı bir
// log kuralı ekler (bkz. EnsureLogRule).
//
// Taramalar ikiye ayrılır:
//   - İnternet geneli: Shodan, Censys gibi tüm interneti tarayan bilinen kuruluşlar
//     (ters DNS kaydı ileri yönde de doğrulanır, sahte PTR kaydıyla kandırılamaz)
//   - Hedefli: geri kalanlar; özellikle gizlenme bayrakları (FIN/NULL/XMAS) kullananlar
//     veya SSH'a da giriş deneyenler
package portscan

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OsmanxCan/kokpit/internal/cfips"
	"github.com/OsmanxCan/kokpit/internal/settings"
)

// Packet engellenmiş tek bir paket.
type Packet struct {
	TS    time.Time
	Src   string
	Port  int
	Proto string // TCP | UDP
	Kind  string // SYN | FIN | NULL | XMAS | UDP | OTHER
}

type Scan struct {
	ID       string   `json:"id"`
	IP       string   `json:"ip"`
	CC       string   `json:"cc"`
	Start    int64    `json:"start"`
	End      int64    `json:"end"`
	Ports    int      `json:"ports"`  // farklı port sayısı
	Sample   []string `json:"sample"` // örnek portlar (en fazla 40, "22/tcp")
	Packets  int      `json:"packets"`
	Types    []string `json:"types"`  // SYN, FIN, NULL, XMAS, UDP
	Kind     string   `json:"kind"`   // internet | targeted | pending
	Reason   string   `json:"reason"` // sınıflandırma gerekçesi
	RDNS     string   `json:"rdns"`
	SSH      bool     `json:"ssh"` // aynı IP SSH'a da giriş denedi
	Banned   bool     `json:"banned"`
	Open     bool     `json:"open"` // tarama sürüyor
	Notified bool     `json:"-"`
}

type Ban struct {
	IP      string `json:"ip"`
	Until   int64  `json:"until"` // 0 = kalıcı
	Created int64  `json:"created"`
	Reason  string `json:"reason"`
	Auto    bool   `json:"auto"`
	By      string `json:"by"`
}

type state struct {
	Scans  []Scan `json:"scans"`
	Bans   []Ban  `json:"bans"`
	LastTS int64  `json:"lastTs"` // son işlenen log zamanı (mikro saniye)
}

type track struct {
	ports   map[string]time.Time // "22/tcp" → son görülme
	all     map[string]bool      // tarama boyunca dokunulan tüm portlar
	kinds   map[string]bool
	packets int
	first   time.Time
	last    time.Time
	scan    *Scan
}

type Engine struct {
	mu     sync.Mutex
	path   string
	st     state
	tracks map[string]*track
	cfg    func() settings.PortScan

	Country   func(ip string) string
	Protected func(ip string) bool // panel oturumu olan IP'ler vb.
	SSHTried  func(ip string) bool // IP son 1 saatte SSH'a giriş denedi mi
	BanFunc   func(ip, comment string) error
	UnbanFunc func(ip string) error
	OnScan    func(Scan) // hedefli tarama bildirimi
	resolver  func(ctx context.Context, ip string) (string, bool)

	logRule  string // "aktif" | hata mesajı
	dirty    bool
	idSeq    int
	lookupMu sync.Mutex
	rdns     map[string]rdnsEntry
}

type rdnsEntry struct {
	name    string
	scanner bool
	at      time.Time
}

func Open(dataDir string, cfg func() settings.PortScan) *Engine {
	e := &Engine{path: filepath.Join(dataDir, "portscan.json"), tracks: map[string]*track{}, cfg: cfg, rdns: map[string]rdnsEntry{}}
	if b, err := os.ReadFile(e.path); err == nil {
		json.Unmarshal(b, &e.st)
	}
	for i := range e.st.Scans {
		e.st.Scans[i].Open = false
		if e.st.Scans[i].Kind == "pending" {
			e.st.Scans[i].Kind = "targeted"
		}
	}
	e.resolver = fcrdns
	return e
}

func (e *Engine) saveLocked() {
	if !e.dirty {
		return
	}
	b, err := json.Marshal(e.st)
	if err != nil {
		return
	}
	tmp := e.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, e.path)
		e.dirty = false
	}
}

// ---------- log ayrıştırma ----------

const logPrefix = "KOKPIT-TARAMA: "

var tcpFlags = map[string]bool{"SYN": true, "ACK": true, "FIN": true, "RST": true, "PSH": true, "URG": true, "CWR": true, "ECE": true}

// ParseLine journalctl -o short-unix satırını çözer. Gelen (IN) ve taramaya benzeyen
// paketleri döner; yanıt paketlerini (ACK, RST) ve ICMP'yi yok sayar.
func ParseLine(line string) (Packet, bool) {
	if !strings.Contains(line, "UFW BLOCK") && !strings.Contains(line, logPrefix) {
		return Packet{}, false
	}
	sp := strings.IndexByte(line, ' ')
	if sp < 0 {
		return Packet{}, false
	}
	sec, err := strconv.ParseFloat(line[:sp], 64)
	if err != nil {
		return Packet{}, false
	}
	p := Packet{TS: time.Unix(0, int64(sec*1e6)*1000)}
	kv := map[string]string{}
	flags := map[string]bool{}
	for _, f := range strings.Fields(line[sp+1:]) {
		if k, v, ok := strings.Cut(f, "="); ok {
			kv[k] = v
		} else if tcpFlags[f] {
			flags[f] = true
		}
	}
	if kv["IN"] == "" || kv["OUT"] != "" {
		return Packet{}, false
	}
	p.Src, p.Proto = kv["SRC"], kv["PROTO"]
	p.Port, _ = strconv.Atoi(kv["DPT"])
	if net.ParseIP(p.Src) == nil || p.Port <= 0 {
		return Packet{}, false
	}
	switch p.Proto {
	case "UDP":
		p.Kind = "UDP"
	case "TCP":
		switch {
		case flags["ACK"] || flags["RST"]:
			return Packet{}, false // kapanmış bağlantının geç gelen paketleri
		case flags["SYN"]:
			p.Kind = "SYN"
		case flags["FIN"] && flags["PSH"] && flags["URG"]:
			p.Kind = "XMAS"
		case flags["FIN"]:
			p.Kind = "FIN"
		case len(flags) == 0:
			p.Kind = "NULL"
		default:
			p.Kind = "OTHER"
		}
	default:
		return Packet{}, false
	}
	return p, true
}

// ---------- tespit ----------

const idleEnd = 5 * time.Minute // bu kadar paket gelmezse tarama bitti sayılır

func stealth(k string) bool { return k == "FIN" || k == "NULL" || k == "XMAS" }

// Feed bir paketi işler. Yeni bir tarama başladığında onu döner.
func (e *Engine) Feed(p Packet) *Scan {
	cfg := e.cfg()
	if cfg.Off {
		return nil
	}
	window := time.Duration(cfg.Window) * time.Second
	e.mu.Lock()
	defer e.mu.Unlock()
	if us := p.TS.UnixMicro(); us > e.st.LastTS {
		e.st.LastTS = us
	}
	t := e.tracks[p.Src]
	if t != nil && p.TS.Sub(t.last) > idleEnd {
		e.closeLocked(p.Src, t)
		t = nil
	}
	if t == nil {
		t = &track{ports: map[string]time.Time{}, all: map[string]bool{}, kinds: map[string]bool{}, first: p.TS}
		e.tracks[p.Src] = t
	}
	key := strconv.Itoa(p.Port) + "/" + strings.ToLower(p.Proto)
	t.last = p.TS
	t.packets++
	t.kinds[p.Kind] = true
	if len(t.ports) < 5000 {
		t.ports[key] = p.TS
	}
	if len(t.all) < 70000 {
		t.all[key] = true
	}
	if t.scan != nil {
		e.updateLocked(t)
		return nil
	}
	// pencere içindeki farklı port sayısı
	n, hasStealth := 0, false
	for k, ts := range t.ports {
		if p.TS.Sub(ts) <= window {
			n++
		} else {
			delete(t.ports, k)
		}
	}
	for k := range t.kinds {
		if stealth(k) {
			hasStealth = true
		}
	}
	if n < cfg.Threshold && !(hasStealth && n >= 3) {
		return nil
	}
	e.idSeq++
	s := &Scan{ID: strconv.FormatInt(t.first.Unix(), 36) + strconv.Itoa(e.idSeq), IP: p.Src, Start: t.first.Unix(), Kind: "pending", Open: true}
	t.scan = s
	e.updateLocked(t)
	cp := *s
	return &cp
}

func (e *Engine) updateLocked(t *track) {
	s := t.scan
	s.End = t.last.Unix()
	s.Ports = len(t.all)
	s.Packets = t.packets
	s.Types = s.Types[:0]
	for _, k := range []string{"SYN", "FIN", "NULL", "XMAS", "UDP", "OTHER"} {
		if t.kinds[k] {
			s.Types = append(s.Types, k)
		}
	}
	// örnek portlar: en küçük numaralılar (iyi bilinen servisler önce görünsün)
	if len(s.Sample) < 40 || t.packets%100 == 0 || !s.Open {
		s.Sample = sample(t.all, 40)
	}
}

func sample(m map[string]bool, max int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, _ := strconv.Atoi(strings.Split(keys[i], "/")[0])
		b, _ := strconv.Atoi(strings.Split(keys[j], "/")[0])
		return a < b
	})
	if len(keys) > max {
		keys = keys[:max]
	}
	return keys
}

// closeLocked biten bir taramayı kaydeder.
func (e *Engine) closeLocked(ip string, t *track) {
	delete(e.tracks, ip)
	if t.scan == nil {
		return
	}
	t.scan.Open = false
	e.updateLocked(t)
	e.storeLocked(*t.scan)
}

func (e *Engine) storeLocked(s Scan) {
	for i := range e.st.Scans {
		if e.st.Scans[i].ID == s.ID {
			// sınıflandırma sonucunu koru
			old := e.st.Scans[i]
			if s.Kind == "pending" {
				s.Kind, s.Reason, s.RDNS, s.SSH, s.CC = old.Kind, old.Reason, old.RDNS, old.SSH, old.CC
			}
			s.Banned = s.Banned || old.Banned
			e.st.Scans[i] = s
			e.dirty = true
			return
		}
	}
	e.st.Scans = append(e.st.Scans, s)
	// 7 günden eski ve 5000'i aşan kayıtları at
	cut := time.Now().Add(-7 * 24 * time.Hour).Unix()
	i := 0
	for i < len(e.st.Scans) && (e.st.Scans[i].End < cut || len(e.st.Scans)-i > 5000) {
		i++
	}
	e.st.Scans = e.st.Scans[i:]
	e.dirty = true
}

// Tick sessiz kalan taramaları kapatır ve süresi dolan engelleri kaldırır.
func (e *Engine) Tick(now time.Time) {
	e.mu.Lock()
	for ip, t := range e.tracks {
		if now.Sub(t.last) > idleEnd {
			e.closeLocked(ip, t)
		}
	}
	var expired []string
	keep := e.st.Bans[:0]
	for _, b := range e.st.Bans {
		if b.Until > 0 && now.Unix() >= b.Until {
			expired = append(expired, b.IP)
			e.dirty = true
			continue
		}
		keep = append(keep, b)
	}
	e.st.Bans = keep
	e.saveLocked()
	unban := e.UnbanFunc
	e.mu.Unlock()
	for _, ip := range expired {
		if unban != nil {
			unban(ip)
		}
	}
}

// ---------- sınıflandırma ----------

// Bilinen, tüm interneti tarayan araştırma/güvenlik kuruluşları (ters DNS son ekleri).
var scannerDomains = []string{
	"shodan.io", "censys-scanner.com", "internet-measurement.com", "shadowserver.org", "binaryedge.ninja",
	"onyphe.net", "stretchoid.com", "recyber.net", "leakix.net", "internettl.org", "alphastrike.io",
	"criminalip.com", "netsystemsresearch.com", "ipip.net", "bitsight.com", "rapid7.com", "arbor-observatory.com",
	"probe.onyphe.net", "intrinsec.com", "quadmetrics.com", "academyforinternetresearch.org", "scan.cypex.ai",
}

func scannerName(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	for _, d := range scannerDomains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// fcrdns ters DNS adını bulur ve ileri yönde doğrular (ad → IP aynı mı).
// Böylece saldırganın kendi IP'sine "scanner.shodan.io" PTR kaydı yazması işe yaramaz.
func fcrdns(ctx context.Context, ip string) (string, bool) {
	r := &net.Resolver{}
	names, err := r.LookupAddr(ctx, ip)
	if err != nil || len(names) == 0 {
		return "", false
	}
	name := strings.TrimSuffix(names[0], ".")
	addrs, err := r.LookupHost(ctx, name)
	if err != nil {
		return name, false
	}
	for _, a := range addrs {
		if a == ip {
			return name, true
		}
	}
	return name, false
}

// Classify taramayı sınıflandırır, gerekiyorsa engeller ve bildirir. Ağ sorguları
// yaptığı için Feed'den ayrı (arka planda) çağrılır.
func (e *Engine) Classify(s Scan) {
	cfg := e.cfg()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	e.lookupMu.Lock()
	ent, ok := e.rdns[s.IP]
	e.lookupMu.Unlock()
	if !ok || time.Since(ent.at) > 24*time.Hour {
		name, verified := e.resolver(ctx, s.IP)
		ent = rdnsEntry{name: name, scanner: verified && scannerName(name), at: time.Now()}
		e.lookupMu.Lock()
		if len(e.rdns) > 20000 {
			e.rdns = map[string]rdnsEntry{}
		}
		e.rdns[s.IP] = ent
		e.lookupMu.Unlock()
	}
	s.RDNS = ent.name
	if e.Country != nil {
		s.CC = e.Country(s.IP)
	}
	if e.SSHTried != nil {
		s.SSH = e.SSHTried(s.IP)
	}
	hasStealth := false
	for _, t := range s.Types {
		if stealth(t) {
			hasStealth = true
		}
	}
	switch {
	case ent.scanner && !s.SSH && !hasStealth:
		s.Kind, s.Reason = "internet", "bilinen internet tarayıcısı ("+ent.name+")"
	case hasStealth:
		s.Kind, s.Reason = "targeted", "gizlenme amaçlı tarama ("+strings.Join(s.Types, ", ")+")"
	case s.SSH:
		s.Kind, s.Reason = "targeted", "SSH'a da giriş denedi"
	default:
		s.Kind, s.Reason = "targeted", "bilinen bir tarayıcı değil"
	}

	ban := cfg.AutoBan && (s.Kind == "targeted" || cfg.BanInternet)
	if ban {
		if err := e.ban(s.IP, "tarama: "+strings.Join(s.Types, ","), cfg.BanHours, true, "otomatik"); err == nil {
			s.Banned = true
		}
	}

	e.mu.Lock()
	set := func(x *Scan) {
		x.Kind, x.Reason, x.RDNS, x.CC, x.SSH = s.Kind, s.Reason, s.RDNS, s.CC, s.SSH
		x.Banned = x.Banned || s.Banned
	}
	if t := e.tracks[s.IP]; t != nil && t.scan != nil && t.scan.ID == s.ID {
		set(t.scan) // tarama sürüyor
		e.updateLocked(t)
		s = *t.scan
	} else {
		found := false
		for i := range e.st.Scans { // bu arada bitmiş
			if e.st.Scans[i].ID == s.ID {
				set(&e.st.Scans[i])
				s, found = e.st.Scans[i], true
			}
		}
		if !found {
			e.storeLocked(s)
		}
		e.dirty = true
	}
	e.mu.Unlock()

	if s.Kind == "targeted" && e.OnScan != nil {
		e.OnScan(s)
	}
}

// ---------- engelleme ----------

var reserved = []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "100.64.0.0/10",
	"::1/128", "fc00::/7", "fe80::/10"}

func inList(ip net.IP, list []string) bool {
	for _, c := range list {
		if _, n, err := net.ParseCIDR(c); err == nil && n.Contains(ip) {
			return true
		}
		if x := net.ParseIP(c); x != nil && x.Equal(ip) {
			return true
		}
	}
	return false
}

// Protected IP'nin hiçbir koşulda engellenmemesi gerekiyorsa nedenini döner.
func (e *Engine) WhyProtected(ipStr string) string {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return "geçersiz IP"
	}
	switch {
	case inList(ip, reserved):
		return "yerel veya özel ağ adresi"
	case cfips.Contains(ip):
		return "Cloudflare adresi (engellenirse siteler erişilemez olur)"
	case inList(ip, e.cfg().Whitelist):
		return "beyaz listede"
	case e.Protected != nil && e.Protected(ipStr):
		return "bu IP'den panele giriş yapılmış"
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.Equal(ip) {
				return "sunucunun kendi adresi"
			}
		}
	}
	return ""
}

func (e *Engine) ban(ip, reason string, hours int, auto bool, by string) error {
	if why := e.WhyProtected(ip); why != "" {
		return fmt.Errorf("%s engellenemez: %s", ip, why)
	}
	e.mu.Lock()
	for _, b := range e.st.Bans {
		if b.IP == ip {
			e.mu.Unlock()
			return nil // zaten engelli
		}
	}
	e.mu.Unlock()
	if e.BanFunc == nil {
		return fmt.Errorf("engelleme kullanılamıyor")
	}
	if err := e.BanFunc(ip, "kokpit: "+reason); err != nil {
		return err
	}
	b := Ban{IP: ip, Created: time.Now().Unix(), Reason: reason, Auto: auto, By: by}
	if hours > 0 {
		b.Until = time.Now().Add(time.Duration(hours) * time.Hour).Unix()
	}
	e.mu.Lock()
	e.st.Bans = append(e.st.Bans, b)
	for i := range e.st.Scans {
		if e.st.Scans[i].IP == ip {
			e.st.Scans[i].Banned = true
		}
	}
	if t := e.tracks[ip]; t != nil && t.scan != nil {
		t.scan.Banned = true
	}
	e.dirty = true
	e.saveLocked()
	e.mu.Unlock()
	return nil
}

// Ban IP'yi elle engeller.
func (e *Engine) Ban(ip, reason string, hours int, by string) error {
	return e.ban(ip, reason, hours, false, by)
}

// Unban engeli kaldırır.
func (e *Engine) Unban(ip string) error {
	e.mu.Lock()
	found := false
	keep := e.st.Bans[:0]
	for _, b := range e.st.Bans {
		if b.IP == ip {
			found = true
			continue
		}
		keep = append(keep, b)
	}
	e.st.Bans = keep
	for i := range e.st.Scans {
		if e.st.Scans[i].IP == ip {
			e.st.Scans[i].Banned = false
		}
	}
	e.dirty = true
	e.saveLocked()
	e.mu.Unlock()
	if !found {
		return fmt.Errorf("%s Kokpit tarafından engellenmemiş", ip)
	}
	if e.UnbanFunc != nil {
		return e.UnbanFunc(ip)
	}
	return nil
}

func (e *Engine) Bans() []Ban {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Ban{}, e.st.Bans...)
}

// ---------- sorgu ----------

type Report struct {
	Scans    []Scan `json:"scans"`
	Internet int    `json:"internet"`
	Targeted int    `json:"targeted"`
	IPs      int    `json:"ips"`
	Banned   int    `json:"banned"`
	Hourly   []int  `json:"hourly"` // hedefli taramalar
	HourlyI  []int  `json:"hourlyInternet"`
	LogRule  string `json:"logRule"`
}

// Report son "since" süresindeki taramaları döner (en yeni başta). Aynı IP'nin
// taramaları tek satırda toplanmaz; her oturum ayrı görünür.
func (e *Engine) Report(since time.Duration, buckets int) Report {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	from := now.Add(-since).Unix()
	all := append([]Scan{}, e.st.Scans...)
	for _, t := range e.tracks {
		if t.scan != nil {
			all = append(all, *t.scan)
		}
	}
	r := Report{Scans: []Scan{}, Hourly: make([]int, buckets), HourlyI: make([]int, buckets), LogRule: e.logRule}
	ips := map[string]bool{}
	bsec := int64(since.Seconds()) / int64(buckets)
	seen := map[string]bool{}
	for i := len(all) - 1; i >= 0; i-- {
		s := all[i]
		if s.End < from || seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		if s.CC == "" && e.Country != nil { // ülke veritabanı sonradan hazır olduysa
			s.CC = e.Country(s.IP)
		}
		r.Scans = append(r.Scans, s)
		ips[s.IP] = true
		idx := buckets - 1 - int((now.Unix()-s.Start)/bsec)
		if idx < 0 {
			idx = 0
		}
		if s.Kind == "internet" {
			r.Internet++
			r.HourlyI[idx]++
		} else {
			r.Targeted++
			r.Hourly[idx]++
		}
		if s.Banned {
			r.Banned++
		}
	}
	sort.SliceStable(r.Scans, func(i, j int) bool { return r.Scans[i].Start > r.Scans[j].Start })
	if len(r.Scans) > 1000 {
		r.Scans = r.Scans[:1000]
	}
	r.IPs = len(ips)
	return r
}

// ---------- canlı log okuma ----------

// Run kernel logunu canlı okur. İlk açılışta son 7 günü, sonra kaldığı yerden devam eder.
func (e *Engine) Run() {
	go func() {
		t := time.NewTicker(30 * time.Second)
		for now := range t.C {
			e.Tick(now)
			e.ensureRule()
		}
	}()
	go func() {
		e.ensureRule()
		for {
			if e.cfg().Off {
				time.Sleep(30 * time.Second)
				continue
			}
			e.mu.Lock()
			since := "-7d"
			last := e.st.LastTS
			if last > 0 {
				since = "@" + strconv.FormatInt(last/1e6, 10)
			}
			e.mu.Unlock()
			cmd := exec.Command("journalctl", "-k", "-f", "-o", "short-unix", "--no-pager", "--since", since,
				"--grep", "UFW BLOCK|"+strings.TrimSpace(logPrefix))
			out, err := cmd.StdoutPipe()
			if err != nil || cmd.Start() != nil {
				time.Sleep(time.Minute)
				continue
			}
			sc := bufio.NewScanner(out)
			sc.Buffer(make([]byte, 64*1024), 256*1024)
			for sc.Scan() {
				if e.cfg().Off {
					break
				}
				p, ok := ParseLine(sc.Text())
				if !ok || p.TS.UnixMicro() <= last { // yeniden başlarken aynı satırları tekrar sayma
					continue
				}
				if s := e.Feed(p); s != nil {
					// geçmiş kayıtlar için ağ sorgusu yapma ve bildirim gönderme
					if time.Since(p.TS) < 15*time.Minute {
						go e.Classify(*s)
					} else {
						e.classifyOld(*s)
					}
				}
			}
			cmd.Process.Kill()
			cmd.Wait()
			time.Sleep(5 * time.Second)
		}
	}()
}

// classifyOld geçmişten okunan taramaları ağ sorgusu yapmadan işaretler.
func (e *Engine) classifyOld(s Scan) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if t := e.tracks[s.IP]; t != nil && t.scan != nil && t.scan.ID == s.ID {
		t.scan.Kind = "targeted"
		t.scan.Reason = "geçmiş kayıt (sınıflandırılmadı)"
		for _, k := range t.scan.Types {
			if stealth(k) {
				t.scan.Reason = "gizlenme amaçlı tarama (" + strings.Join(t.scan.Types, ", ") + ")"
			}
		}
		if e.Country != nil {
			t.scan.CC = e.Country(s.IP)
		}
	}
}

// ensureRule engellenen paketler için IP başına sınırlı log kuralını ekler.
// UFW yeniden yüklendiğinde kural silinebileceği için düzenli olarak kontrol edilir.
func (e *Engine) ensureRule() {
	status := EnsureLogRule(!e.cfg().Off)
	e.mu.Lock()
	e.logRule = status
	e.mu.Unlock()
}

var ruleArgs = []string{"-m", "hashlimit", "--hashlimit-upto", "120/minute", "--hashlimit-burst", "200",
	"--hashlimit-mode", "srcip", "--hashlimit-name", "kokpit_tarama",
	"-m", "limit", "--limit", "3000/minute", "--limit-burst", "3000",
	"-j", "LOG", "--log-prefix", logPrefix, "--log-level", "6"}

// EnsureLogRule log kuralını ekler (on) veya kaldırır. Durumu açıklayan bir metin döner:
// "aktif", "kapalı" veya hata.
func EnsureLogRule(on bool) string {
	type fam struct{ bin, chain string }
	fams := []fam{{"iptables", "ufw-after-logging-input"}, {"ip6tables", "ufw6-after-logging-input"}}
	status := "aktif"
	for _, f := range fams {
		if _, err := exec.LookPath(f.bin); err != nil {
			return "iptables bulunamadı; UFW'nin kendi (dakikada birkaç satırla sınırlı) logları kullanılıyor"
		}
		args := append([]string{"-w", "-C", f.chain}, ruleArgs...)
		exists := exec.Command(f.bin, args...).Run() == nil
		switch {
		case on && !exists:
			args[1] = "-I"
			args = append(args[:3], append([]string{"1"}, args[3:]...)...)
			if out, err := exec.Command(f.bin, args...).CombinedOutput(); err != nil {
				if f.bin == "ip6tables" {
					continue // IPv6 yoksa sorun değil
				}
				msg := strings.TrimSpace(string(out))
				if strings.Contains(msg, "No chain") || strings.Contains(msg, "doesn't exist") {
					return "UFW kapalı olduğu için log kuralı eklenemedi"
				}
				return "log kuralı eklenemedi: " + msg
			}
		case !on && exists:
			args[1] = "-D"
			exec.Command(f.bin, args...).Run()
			status = "kapalı"
		case !on:
			status = "kapalı"
		}
	}
	return status
}
