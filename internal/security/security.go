// Package security sunucunun güvenlik durumunu analiz eder.
package security

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OsmanxCan/kokpit/internal/execx"
)

type Check struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	OK     bool   `json:"ok"`
	Level  string `json:"level"` // critical | warning | info
	Detail string `json:"detail"`
	Fix    string `json:"fix"`
	Weight int    `json:"weight"`
}

type Port struct {
	Proto   string `json:"proto"`
	Addr    string `json:"addr"`
	Port    int    `json:"port"`
	Process string `json:"process"`
	Public  bool   `json:"public"`
}

type SSHEvent struct {
	TS     string `json:"ts"`
	Type   string `json:"type"` // accepted | failed | invalid
	User   string `json:"user"`
	IP     string `json:"ip"`
	Method string `json:"method"`
}

type IPCount struct {
	IP    string `json:"ip"`
	Count int    `json:"count"`
	Users string `json:"users"`
}

type Session struct {
	User string `json:"user"`
	TTY  string `json:"tty"`
	From string `json:"from"`
	When string `json:"when"`
}

type Report struct {
	TS          int64      `json:"ts"`
	Score       int        `json:"score"`
	Checks      []Check    `json:"checks"`
	Ports       []Port     `json:"ports"`
	SSHAccepted []SSHEvent `json:"sshAccepted"`
	SSHFailTop  []IPCount  `json:"sshFailTop"`
	SSHFail24h  int        `json:"sshFail24h"`
	Sessions    []Session  `json:"sessions"`
	Banned      []string   `json:"banned"`
	Fail2ban    bool       `json:"fail2ban"`
	UFW         string     `json:"ufw"`
	UFWRules    string     `json:"ufwRules"`
	Updates     int        `json:"updates"`
	SecUpdates  int        `json:"secUpdates"`
	Reboot      bool       `json:"reboot"`
}

type Analyzer struct {
	mu       sync.Mutex
	last     *Report
	updTS    time.Time
	upd, sec int
	RootPM2  func() []string // root olarak çalışan PM2 süreçleri (dışarıdan verilir)
}

func New() *Analyzer { return &Analyzer{} }

// Get son raporu döner; maxAge'den eskiyse yeniden hesaplar.
func (a *Analyzer) Get(maxAge time.Duration) *Report {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.last != nil && time.Since(time.Unix(a.last.TS, 0)) < maxAge {
		return a.last
	}
	a.last = a.build()
	return a.last
}

func (a *Analyzer) build() *Report {
	r := &Report{TS: time.Now().Unix()}
	sshd := sshdConfig()
	r.Ports = ListeningPorts()
	r.UFW, r.UFWRules = ufwStatus()
	r.Fail2ban = execx.Exists("fail2ban-client")
	if r.Fail2ban {
		r.Banned = Fail2banBanned()
	}
	r.SSHAccepted, r.SSHFailTop, r.SSHFail24h = sshEvents()
	r.Sessions = whoSessions()
	if time.Since(a.updTS) > 6*time.Hour {
		a.upd, a.sec = pendingUpdates()
		a.updTS = time.Now()
	}
	r.Updates, r.SecUpdates = a.upd, a.sec
	_, err := os.Stat("/var/run/reboot-required")
	r.Reboot = err == nil

	add := func(c Check) { r.Checks = append(r.Checks, c) }

	pwd := strings.ToLower(sshd["passwordauthentication"])
	add(Check{ID: "ssh_password", Title: "SSH şifre ile giriş kapalı", OK: pwd == "no", Level: "critical", Weight: 20,
		Detail: "passwordauthentication = " + orDash(pwd),
		Fix:    "SSH anahtarı ekleyip /etc/ssh/sshd_config.d/00-kokpit.conf içine 'PasswordAuthentication no' yaz (sertlestir.sh bunu yapar)."})
	root := strings.ToLower(sshd["permitrootlogin"])
	add(Check{ID: "ssh_root", Title: "Root SSH girişi sadece anahtarla", OK: root == "no" || root == "prohibit-password" || root == "without-password", Level: "critical", Weight: 15,
		Detail: "permitrootlogin = " + orDash(root), Fix: "'PermitRootLogin prohibit-password' yap."})
	add(Check{ID: "ssh_tries", Title: "SSH deneme sınırı düşük (MaxAuthTries ≤ 4)", OK: atoi(sshd["maxauthtries"]) > 0 && atoi(sshd["maxauthtries"]) <= 4, Level: "info", Weight: 3,
		Detail: "maxauthtries = " + orDash(sshd["maxauthtries"]), Fix: "'MaxAuthTries 3' ekle."})
	add(Check{ID: "firewall", Title: "Firewall (UFW) aktif", OK: r.UFW == "active", Level: "critical", Weight: 20,
		Detail: "ufw: " + orDash(r.UFW), Fix: "sertlestir.sh ile kuralları ekleyip UFW'yi aç."})
	add(Check{ID: "fail2ban", Title: "Brute-force koruması (fail2ban) kurulu", OK: r.Fail2ban, Level: "warning", Weight: 10,
		Detail: map[bool]string{true: "kurulu", false: "kurulu değil"}[r.Fail2ban], Fix: "apt install fail2ban (sertlestir.sh yapar)."})

	var exposed []string
	for _, p := range r.Ports {
		if p.Public && !expectedPublic(p) {
			exposed = append(exposed, p.Proto+"/"+strconv.Itoa(p.Port)+" ("+p.Process+")")
		}
	}
	add(Check{ID: "exposed_ports", Title: "Gereksiz yere dışarı açık uygulama portu yok", OK: len(exposed) == 0, Level: "warning", Weight: 10,
		Detail: orDash(strings.Join(exposed, ", ")), Fix: "Uygulamayı 127.0.0.1'e bağla (örn. next start -H 127.0.0.1) veya firewall ile kapat."})

	add(Check{ID: "updates", Title: "Bekleyen güvenlik güncellemesi yok", OK: r.SecUpdates == 0, Level: "warning", Weight: 8,
		Detail: strconv.Itoa(r.SecUpdates) + " güvenlik / " + strconv.Itoa(r.Updates) + " toplam güncelleme", Fix: "apt update && apt upgrade"})
	add(Check{ID: "reboot", Title: "Yeniden başlatma beklemiyor", OK: !r.Reboot, Level: "info", Weight: 3,
		Detail: map[bool]string{true: "çekirdek/kütüphane güncellemesi için reboot gerekiyor", false: "gerek yok"}[r.Reboot], Fix: "Uygun bir zamanda reboot."})
	_, uaErr := os.Stat("/etc/apt/apt.conf.d/20auto-upgrades")
	add(Check{ID: "auto_updates", Title: "Otomatik güvenlik güncellemeleri açık", OK: uaErr == nil, Level: "info", Weight: 4,
		Detail: map[bool]string{true: "unattended-upgrades ayarlı", false: "ayarlı değil"}[uaErr == nil], Fix: "dpkg-reconfigure -plow unattended-upgrades"})
	if a.RootPM2 != nil {
		rp := a.RootPM2()
		add(Check{ID: "pm2_root", Title: "Web uygulamaları root olarak çalışmıyor", OK: len(rp) == 0, Level: "warning", Weight: 7,
			Detail: orDash(strings.Join(rp, ", ")), Fix: "Uygulamayı ayrı bir kullanıcıyla (örn. 'web') PM2 altında çalıştır."})
	}

	total, got := 0, 0
	for _, c := range r.Checks {
		total += c.Weight
		if c.OK {
			got += c.Weight
		}
	}
	if total > 0 {
		r.Score = got * 100 / total
	}
	return r
}

func expectedPublic(p Port) bool {
	switch p.Process {
	case "sshd", "nginx", "docker-proxy", "openvpn", "cloudflared", "systemd", "caddy", "apache2":
		return true
	}
	if p.Proto == "udp" && p.Process == "" { // çekirdek WireGuard
		return true
	}
	return false
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func sshdConfig() map[string]string {
	m := map[string]string{}
	out, err := execx.Run("sshd", "-T")
	if err != nil {
		out, _ = execx.Run("/usr/sbin/sshd", "-T")
	}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 {
			if _, ok := m[f[0]]; !ok {
				m[f[0]] = strings.Join(f[1:], " ")
			}
		}
	}
	return m
}

var procRe = regexp.MustCompile(`users:\(\("([^"]+)"`)

func ListeningPorts() []Port {
	out, _ := execx.Run("ss", "-tulpnH")
	seen := map[string]bool{}
	var ports []Port
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 5 {
			continue
		}
		proto := f[0]
		if proto != "tcp" && proto != "udp" {
			continue
		}
		if proto == "tcp" && f[1] != "LISTEN" {
			continue
		}
		local := f[4]
		i := strings.LastIndex(local, ":")
		if i < 0 {
			continue
		}
		addr, portS := local[:i], local[i+1:]
		port, _ := strconv.Atoi(portS)
		proc := ""
		if m := procRe.FindStringSubmatch(l); m != nil {
			proc = m[1]
		}
		addr = strings.Trim(addr, "[]")
		if j := strings.Index(addr, "%"); j >= 0 {
			addr = addr[:j]
		}
		public := addr == "0.0.0.0" || addr == "*" || addr == "::"
		if !public {
			if ip := net.ParseIP(addr); ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsPrivate() {
				public = true
			}
		}
		key := proto + portS + proc + strconv.FormatBool(public)
		if seen[key] {
			continue
		}
		seen[key] = true
		ports = append(ports, Port{proto, addr, port, proc, public})
	}
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Public != ports[j].Public {
			return ports[i].Public
		}
		return ports[i].Port < ports[j].Port
	})
	return ports
}

func ufwStatus() (string, string) {
	if !execx.Exists("ufw") {
		return "kurulu değil", ""
	}
	out, _ := execx.Run("ufw", "status", "verbose")
	st := "bilinmiyor"
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "Status:") || strings.HasPrefix(l, "Durum:") {
			st = strings.TrimSpace(strings.SplitN(l, ":", 2)[1])
		}
	}
	return st, out
}

func Fail2banBanned() []string {
	out, _ := execx.Run("fail2ban-client", "status", "sshd")
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "Banned IP list:") {
			s := strings.TrimSpace(strings.SplitN(l, ":", 2)[1])
			if s == "" {
				return []string{}
			}
			return strings.Fields(s)
		}
	}
	return []string{}
}

// Ban/Unban fail2ban sshd jail'i üzerinden.
func Ban(ip string, ban bool) (string, error) {
	if net.ParseIP(ip) == nil {
		return "", errors.New("geçersiz IP")
	}
	if !execx.Exists("fail2ban-client") {
		return "", errors.New("fail2ban kurulu değil")
	}
	cmd := "banip"
	if !ban {
		cmd = "unbanip"
	}
	return execx.Run("fail2ban-client", "set", "sshd", cmd, ip)
}

var (
	acceptedRe = regexp.MustCompile(`Accepted (\S+) for (\S+) from (\S+)`)
	failedRe   = regexp.MustCompile(`Failed (?:password|publickey) for (?:invalid user )?(\S+) from (\S+)`)
	invalidRe  = regexp.MustCompile(`Invalid user (\S*) from (\S+)`)
)

// SSHScan journal'daki SSH olaylarını satır satır okur ve her olay için fn'i çağırır.
// Çıktı belleğe topluca alınmaz; saldırı alan sunucularda log'lar yüz MB'ları bulabilir.
// pattern boş değilse journalctl --grep ile önceden süzülür (çok daha hızlı).
func SSHScan(since, pattern string, fn func(SSHEvent)) {
	if !scanJournal(since, pattern, fn) && pattern != "" {
		// Eski journalctl --grep desteklemiyorsa süzmeden oku
		scanJournal(since, "", fn)
	}
}

func scanJournal(since, pattern string, fn func(SSHEvent)) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	args := []string{"-t", "sshd", "-t", "sshd-session", "--since", since, "--no-pager", "-o", "short-iso", "-q"}
	if pattern != "" {
		args = append(args, "--grep", pattern)
	}
	cmd := exec.CommandContext(ctx, "journalctl", args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return false
	}
	if err := cmd.Start(); err != nil {
		return false
	}
	n := 0
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		n++
		if e, ok := parseSSH(sc.Text()); ok {
			fn(e)
		}
	}
	err = cmd.Wait()
	// journalctl --grep eşleşme yoksa 1 ile çıkar; bu bir hata değil
	if err != nil && n == 0 && pattern != "" {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return true
		}
		return false
	}
	return true
}

func parseSSH(l string) (SSHEvent, bool) {
	ts := ""
	if i := strings.IndexByte(l, ' '); i > 0 {
		ts = l[:i]
	}
	if m := acceptedRe.FindStringSubmatch(l); m != nil {
		return SSHEvent{ts, "accepted", m[2], m[3], m[1]}, true
	}
	if m := failedRe.FindStringSubmatch(l); m != nil {
		return SSHEvent{ts, "failed", m[1], m[2], ""}, true
	}
	if m := invalidRe.FindStringSubmatch(l); m != nil {
		return SSHEvent{ts, "invalid", m[1], m[2], ""}, true
	}
	return SSHEvent{}, false
}

const sshPattern = "Accepted |Failed (password|publickey)|Invalid user"

// SSHLog kısa süreli aralıklar için olayları liste olarak döner (en fazla 20.000).
func SSHLog(since string) []SSHEvent {
	var ev []SSHEvent
	SSHScan(since, sshPattern, func(e SSHEvent) {
		if len(ev) < 20000 {
			ev = append(ev, e)
		}
	})
	return ev
}

// SSHAcceptedIPs başarılı giriş yapılan IP'leri döner.
func SSHAcceptedIPs(since string) map[string]bool {
	ips := map[string]bool{}
	SSHScan(since, "Accepted ", func(e SSHEvent) {
		if e.Type == "accepted" {
			ips[e.IP] = true
		}
	})
	return ips
}

func sshEvents() ([]SSHEvent, []IPCount, int) {
	accepted := []SSHEvent{}
	fails := map[string]*IPCount{}
	users := map[string]map[string]bool{}
	nfail := 0
	SSHScan("-24h", sshPattern, func(e SSHEvent) {
		if e.Type == "accepted" {
			accepted = append(accepted, e)
			if len(accepted) > 60 {
				accepted = accepted[len(accepted)-30:]
			}
			return
		}
		nfail++
		c, ok := fails[e.IP]
		if !ok {
			if len(fails) >= 50000 { // aşırı durumda belleği sınırla
				return
			}
			c = &IPCount{IP: e.IP}
			fails[e.IP] = c
			users[e.IP] = map[string]bool{}
		}
		c.Count++
		if len(users[e.IP]) < 5 {
			users[e.IP][e.User] = true
		}
	})
	top := []IPCount{}
	for ip, c := range fails {
		var us []string
		for u := range users[ip] {
			us = append(us, u)
		}
		sort.Strings(us)
		c.Users = strings.Join(us, ", ")
		top = append(top, *c)
	}
	sort.Slice(top, func(i, j int) bool { return top[i].Count > top[j].Count })
	if len(top) > 25 {
		top = top[:25]
	}
	// en yeni başarılı girişler önce
	for i, j := 0, len(accepted)-1; i < j; i, j = i+1, j-1 {
		accepted[i], accepted[j] = accepted[j], accepted[i]
	}
	if len(accepted) > 30 {
		accepted = accepted[:30]
	}
	return accepted, top, nfail
}

func whoSessions() []Session {
	out, _ := execx.Run("who")
	s := []Session{}
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(l)
		if len(f) < 4 {
			continue
		}
		from := ""
		if i := strings.Index(l, "("); i >= 0 {
			from = strings.Trim(l[i:], "()")
		}
		s = append(s, Session{f[0], f[1], from, f[2] + " " + f[3]})
	}
	return s
}

func pendingUpdates() (int, int) {
	if !execx.Exists("apt") {
		return 0, 0
	}
	out, _ := execx.RunTimeout(60*time.Second, "apt", "list", "--upgradable")
	total, sec := 0, 0
	for _, l := range strings.Split(out, "\n") {
		if !strings.Contains(l, "upgradable") {
			continue
		}
		total++
		if strings.Contains(l, "-security") {
			sec++
		}
	}
	return total, sec
}
