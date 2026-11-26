package weblog

import (
	"bufio"
	"compress/gzip"
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OsmanxCan/kokpit/internal/nginx"
)

// ---------- satır ayrıştırma ----------

type Entry struct {
	IP     string
	TS     time.Time
	Method string
	Path   string // ham (kodlanmış) yol + sorgu
	Status int
	Bytes  int64
	UA     string
	Site   string
}

// combined: $remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent"
var lineRe = regexp.MustCompile(`^(\S+) \S+ \S+ \[([^\]]+)\] "((?:[^"\\]|\\.)*)" (\d{3}) (\d+|-)(?: "((?:[^"\\]|\\.)*)" "((?:[^"\\]|\\.)*)")?`)

func ParseLine(l string) (Entry, bool) {
	m := lineRe.FindStringSubmatch(l)
	if m == nil {
		return Entry{}, false
	}
	ts, err := time.Parse("02/Jan/2006:15:04:05 -0700", m[2])
	if err != nil {
		return Entry{}, false
	}
	e := Entry{IP: m[1], TS: ts, UA: m[7]}
	e.Status, _ = strconv.Atoi(m[4])
	e.Bytes, _ = strconv.ParseInt(m[5], 10, 64)
	req := m[3]
	if method, rest, ok := strings.Cut(req, " "); ok {
		e.Method = method
		e.Path, _, _ = strings.Cut(rest, " ")
	} else {
		e.Path = req // geçersiz istek (örn. TLS el sıkışması)
	}
	return e, true
}

// ---------- saldırı kategorileri ----------

type category struct {
	ID, Name string
	match    func(path, ua string) bool
}

func anyContains(s string, list ...string) bool {
	for _, x := range list {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

// sensitive hassas dosya yolları; 200 dönerse dışarı sızıyor olabilir.
var sensitive = []string{"/.env", "/.git/", "/.git", "/.svn/", "/.hg/", "/.aws/", "/.ssh/", "/id_rsa", "/.htpasswd", "/.htaccess",
	"/.ds_store", "/wp-config.php.", "/config.php.bak", "/web.config", "/.npmrc", "/.dockercfg", "/docker-compose.yml",
	"/backup.zip", "/backup.sql", "/backup.tar", "/dump.sql", "/database.sql", "/db.sql", "/.vscode/", "/.idea/",
	"/phpinfo.php", "/info.php", "/server-status", "/.bash_history", "/credentials", "/secrets.json"}

var categories = []category{
	{"rce", "Komut çalıştırma denemesi", func(p, ua string) bool {
		return anyContains(p, "${jndi:", "/bin/sh", "/bin/bash", "cmd.exe", ";wget ", ";curl ", "|sh", "shell_exec", "base64_decode(", "eval(",
			"system(", "passthru(", "/cgi-bin/", "${${", "() { :;};", "invokefunction", "allow_url_include", "/vendor/phpunit/") ||
			anyContains(ua, "${jndi:", "() { :;};")
	}},
	{"sqli", "SQL injection", func(p, ua string) bool {
		return anyContains(p, "union select", "union all select", "' or '1'='1", "' or 1=1", "\" or 1=1", "or 1=1--", "sleep(", "benchmark(",
			"information_schema", "pg_sleep", "waitfor delay", "extractvalue(", "updatexml(", "load_file(", "into outfile")
	}},
	{"traversal", "Dizin dolaşma", func(p, ua string) bool {
		return anyContains(p, "../", "..\\", "/etc/passwd", "/etc/shadow", "win.ini", "/proc/self/", "boot.ini")
	}},
	{"xss", "XSS", func(p, ua string) bool {
		return anyContains(p, "<script", "javascript:", "onerror=", "onload=", "<svg", "alert(", "document.cookie")
	}},
	{"sensitive", "Hassas dosya arama", func(p, ua string) bool {
		path, _, _ := strings.Cut(p, "?")
		for _, s := range sensitive {
			if strings.HasPrefix(path, s) || strings.Contains(path, s) {
				return true
			}
		}
		return false
	}},
	{"wordpress", "WordPress taraması", func(p, ua string) bool {
		return anyContains(p, "/wp-login.php", "/wp-admin", "/xmlrpc.php", "/wp-content/", "/wp-includes/", "/wp-json/", "/wordpress/")
	}},
	{"admin", "Yönetim paneli arama", func(p, ua string) bool {
		return anyContains(p, "/phpmyadmin", "/pma/", "/myadmin", "/adminer", "/manager/html", "/solr/", "/actuator", "/_ignition",
			"/console/", "/jenkins", "/hudson", "/telescope", "/horizon/", "/.well-known/security.txt.bak", "/owa/", "/ecp/", "/autodiscover",
			"/remote/login", "/boaform", "/hnap1", "/setup.cgi", "/goform/", "/geoserver", "/druid/", "/nacos")
	}},
	{"tool", "Saldırı aracı", func(p, ua string) bool {
		return anyContains(ua, "sqlmap", "nikto", "nuclei", "masscan", "zgrab", "nmap", "wpscan", "dirbuster", "gobuster", "ffuf",
			"feroxbuster", "acunetix", "nessus", "openvas", "hydra", "whatweb", "jaeles", "dirsearch", "netsparker", "burp")
	}},
}

var catNames = func() map[string]string {
	m := map[string]string{"notfound": "Çok fazla 404", "invalid": "Geçersiz istek"}
	for _, c := range categories {
		m[c.ID] = c.Name
	}
	return m
}()

func decode(p string) string {
	for i := 0; i < 2; i++ {
		if d, err := url.QueryUnescape(p); err == nil {
			p = d
		}
	}
	return strings.ToLower(strings.ReplaceAll(p, "+", " "))
}

// Classify isteğin saldırı kategorilerini döner (boş = normal).
func Classify(e Entry) []string {
	if e.Method == "" && e.Path != "" {
		return []string{"invalid"}
	}
	p := decode(e.Path)
	ua := strings.ToLower(e.UA)
	var out []string
	for _, c := range categories {
		if c.match(p, ua) {
			out = append(out, c.ID)
		}
	}
	return out
}

// ---------- rapor ----------

type IPStat struct {
	IP       string         `json:"ip"`
	CC       string         `json:"cc"`
	Requests int            `json:"requests"`
	Attacks  int            `json:"attacks"`
	NotFound int            `json:"notFound"`
	Cats     map[string]int `json:"cats"`
	Sites    []string       `json:"sites"`
	Paths    []string       `json:"paths"` // örnek saldırı yolları
	UA       string         `json:"ua"`
	First    int64          `json:"first"`
	Last     int64          `json:"last"`
	Blocked  string         `json:"blocked"` // "" | cloudflare | firewall
}

type Hit struct {
	TS     int64    `json:"ts"`
	IP     string   `json:"ip"`
	CC     string   `json:"cc"`
	Method string   `json:"method"`
	Path   string   `json:"path"`
	Status int      `json:"status"`
	Cats   []string `json:"cats"`
	Site   string   `json:"site"`
}

// Finding hassas bir dosyanın dışarıdan okunabildiği şüphesi.
type Finding struct {
	Site      string `json:"site"`
	Path      string `json:"path"`
	Status    int    `json:"status"`
	Bytes     int64  `json:"bytes"`
	Count     int    `json:"count"`
	Last      int64  `json:"last"`
	Confirmed bool   `json:"confirmed"` // içerik kontrolüyle doğrulandı
	URL       string `json:"url"`
	Note      string `json:"note"`
}

type Report struct {
	Range      string            `json:"range"`
	Generated  int64             `json:"generated"`
	Requests   int               `json:"requests"`
	Attacks    int               `json:"attacks"`
	AttackIPs  int               `json:"attackIps"`
	Cats       map[string]int    `json:"cats"`
	CatNames   map[string]string `json:"catNames"`
	Sites      map[string]int    `json:"sites"` // site → saldırı sayısı
	TopIPs     []IPStat          `json:"topIps"`
	Recent     []Hit             `json:"recent"`
	Hourly     []int             `json:"hourly"`
	Findings   []Finding         `json:"findings"`
	Files      []string          `json:"files"`
	LocalShare float64           `json:"localShare"` // isteklerin yüzde kaçı 127.0.0.1'den (gerçek IP ayarı eksik)
}

var rotatedRe = regexp.MustCompile(`^\.\d+(\.gz)?$`)

type logFile struct {
	path, site string
}

// logFiles Nginx'in yazdığı access loglarını ve döndürülmüş kopyalarını bulur.
func logFiles(since time.Time) []logFile {
	bases := map[string]string{"/var/log/nginx/access.log": ""}
	if sites, err := nginx.Sites(); err == nil {
		for _, s := range sites {
			if s.AccessLog != "" && len(s.Names) > 0 {
				bases[s.AccessLog] = s.Names[0]
			}
		}
	}
	var out []logFile
	for base, site := range bases {
		cands, _ := filepath.Glob(base + "*")
		for _, c := range cands {
			rest := strings.TrimPrefix(c, base)
			if rest != "" && !rotatedRe.MatchString(rest) {
				continue
			}
			if fi, err := os.Stat(c); err == nil && fi.ModTime().After(since) {
				out = append(out, logFile{c, site})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

func open(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(path, ".gz") {
		return f, nil
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{gz, f}, nil
}

type Analyzer struct {
	mu       sync.Mutex
	cache    map[string]*Report
	Country  func(string) string
	Blocked  func(ip string) string // engellenmiş mi (cloudflare / firewall)
	OnFind   func(Finding)          // doğrulanmış sızıntı bildirimi
	files    func(time.Time) []logFile
	verify   func(Finding, []string) Finding
	notified map[string]time.Time
}

func NewAnalyzer() *Analyzer {
	return &Analyzer{cache: map[string]*Report{}, files: logFiles, verify: verifyFinding, notified: map[string]time.Time{}}
}

// Report son 24 saat ("24h") veya 7 gün ("7d") için raporu döner (10 dakika önbellekli).
func (a *Analyzer) Report(rng string, fresh bool) *Report {
	a.mu.Lock()
	if c := a.cache[rng]; c != nil && !fresh && time.Since(time.Unix(c.Generated, 0)) < 10*time.Minute {
		a.mu.Unlock()
		return a.decorate(c)
	}
	a.mu.Unlock()
	r := a.build(rng)
	a.mu.Lock()
	a.cache[rng] = r
	a.mu.Unlock()
	return a.decorate(r)
}

// decorate engel durumunu günceller (önbellekteki rapor için de güncel olsun).
func (a *Analyzer) decorate(r *Report) *Report {
	if a.Blocked == nil {
		return r
	}
	cp := *r
	cp.TopIPs = append([]IPStat{}, r.TopIPs...)
	for i := range cp.TopIPs {
		cp.TopIPs[i].Blocked = a.Blocked(cp.TopIPs[i].IP)
	}
	return &cp
}

func (a *Analyzer) build(rng string) *Report {
	since, buckets, bsec := 24*time.Hour, 24, int64(3600)
	if rng == "7d" {
		since, buckets, bsec = 7*24*time.Hour, 7, 86400
	}
	now := time.Now()
	from := now.Add(-since)
	r := &Report{Range: rng, Generated: now.Unix(), Cats: map[string]int{}, CatNames: catNames, Sites: map[string]int{},
		Hourly: make([]int, buckets), Recent: []Hit{}, Findings: []Finding{}, TopIPs: []IPStat{}}
	type agg struct {
		st    IPStat
		paths map[string]bool
		sites map[string]bool
	}
	ips := map[string]*agg{}
	type fkey struct{ site, path string }
	finds := map[fkey]*Finding{}
	local := 0
	end := now.Unix()/bsec*bsec + bsec
	for _, lf := range a.files(from) {
		rc, err := open(lf.path)
		if err != nil {
			continue
		}
		r.Files = append(r.Files, lf.path)
		sc := bufio.NewScanner(rc)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			e, ok := ParseLine(sc.Text())
			if !ok || e.TS.Before(from) {
				continue
			}
			e.Site = lf.site
			r.Requests++
			if e.IP == "127.0.0.1" || e.IP == "::1" {
				local++
				continue // gerçek IP bilinmiyor; saldırgan olarak sayma
			}
			cats := Classify(e)
			x := ips[e.IP]
			if x == nil {
				if len(ips) >= 200000 {
					continue
				}
				x = &agg{st: IPStat{IP: e.IP, Cats: map[string]int{}, First: e.TS.Unix()}, paths: map[string]bool{}, sites: map[string]bool{}}
				ips[e.IP] = x
			}
			x.st.Requests++
			x.st.Last = e.TS.Unix()
			if e.Status == 404 {
				x.st.NotFound++
			}
			if len(cats) == 0 {
				continue
			}
			r.Attacks++
			x.st.Attacks++
			if x.st.UA == "" {
				x.st.UA = e.UA
			}
			for _, c := range cats {
				r.Cats[c]++
				x.st.Cats[c]++
			}
			site := e.Site
			if site == "" {
				site = "genel log"
			}
			r.Sites[site]++
			x.sites[site] = true
			if len(x.paths) < 12 {
				x.paths[truncate(e.Method+" "+e.Path, 160)] = true
			}
			if i := len(r.Hourly) - 1 - int((end-1-e.TS.Unix())/bsec); i >= 0 && i < len(r.Hourly) {
				r.Hourly[i]++
			}
			r.Recent = append(r.Recent, Hit{TS: e.TS.Unix(), IP: e.IP, Method: e.Method, Path: truncate(e.Path, 200), Status: e.Status, Cats: cats, Site: e.Site})
			if len(r.Recent) > 400 {
				r.Recent = r.Recent[len(r.Recent)-200:]
			}
			// hassas dosya 200 döndüyse şüpheli
			if e.Status == 200 && e.Bytes > 0 && contains(cats, "sensitive") {
				p, _, _ := strings.Cut(e.Path, "?")
				k := fkey{e.Site, p}
				f := finds[k]
				if f == nil {
					if len(finds) >= 50 {
						continue
					}
					f = &Finding{Site: e.Site, Path: p, Status: e.Status, Bytes: e.Bytes}
					finds[k] = f
				}
				f.Count++
				f.Last = e.TS.Unix()
			}
		}
		rc.Close()
	}
	if r.Requests > 0 {
		r.LocalShare = float64(local) / float64(r.Requests) * 100
	}
	// IP'ler: saldırı yapan veya çok 404 üretenler
	var list []IPStat
	for _, x := range ips {
		if x.st.NotFound >= 30 {
			x.st.Cats["notfound"] = x.st.NotFound
		}
		if x.st.Attacks == 0 && x.st.NotFound < 30 {
			continue
		}
		for p := range x.paths {
			x.st.Paths = append(x.st.Paths, p)
		}
		sort.Strings(x.st.Paths)
		for s := range x.sites {
			x.st.Sites = append(x.st.Sites, s)
		}
		sort.Strings(x.st.Sites)
		if a.Country != nil {
			x.st.CC = a.Country(x.st.IP)
		}
		list = append(list, x.st)
	}
	r.AttackIPs = len(list)
	sort.Slice(list, func(i, j int) bool {
		if list[i].Attacks != list[j].Attacks {
			return list[i].Attacks > list[j].Attacks
		}
		return list[i].NotFound > list[j].NotFound
	})
	if len(list) > 100 {
		list = list[:100]
	}
	r.TopIPs = append(r.TopIPs, list...)
	// son saldırılar (en yeni başta)
	sort.SliceStable(r.Recent, func(i, j int) bool { return r.Recent[i].TS > r.Recent[j].TS })
	if len(r.Recent) > 100 {
		r.Recent = r.Recent[:100]
	}
	for i := range r.Recent {
		if a.Country != nil {
			r.Recent[i].CC = a.Country(r.Recent[i].IP)
		}
	}
	// şüpheli sızıntıları içerikten doğrula
	var hosts []string
	if sites, err := nginx.Sites(); err == nil {
		for _, s := range sites {
			for _, n := range s.Names {
				if strings.Contains(n, ".") && !strings.HasPrefix(n, "*") && !strings.Contains(n, " ") {
					hosts = append(hosts, n)
				}
			}
		}
	}
	for _, f := range finds {
		v := a.verify(*f, hosts)
		r.Findings = append(r.Findings, v)
		if v.Confirmed && a.OnFind != nil {
			k := v.URL
			a.mu.Lock()
			last := a.notified[k]
			if time.Since(last) > 24*time.Hour {
				a.notified[k] = time.Now()
				a.mu.Unlock()
				a.OnFind(v)
			} else {
				a.mu.Unlock()
			}
		}
	}
	sort.Slice(r.Findings, func(i, j int) bool { return r.Findings[i].Confirmed && !r.Findings[j].Confirmed })
	return r
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// ---------- doğrulama ----------

// signature yola göre gerçek bir sızıntıda içerikte olması beklenen kalıp.
func signature(path string) *regexp.Regexp {
	p := strings.ToLower(path)
	switch {
	case strings.Contains(p, ".env"):
		return regexp.MustCompile(`(?m)^[A-Z][A-Z0-9_]{2,}=`)
	case strings.Contains(p, ".git"):
		return regexp.MustCompile(`\[core\]|ref: refs/|repositoryformatversion`)
	case strings.Contains(p, ".sql"):
		return regexp.MustCompile(`(?i)CREATE TABLE|INSERT INTO|-- MySQL dump|PostgreSQL database dump`)
	case strings.Contains(p, "phpinfo") || strings.Contains(p, "info.php"):
		return regexp.MustCompile(`PHP Version|phpinfo\(\)`)
	case strings.Contains(p, "server-status"):
		return regexp.MustCompile(`Apache Server Status|Server uptime`)
	case strings.Contains(p, "id_rsa") || strings.Contains(p, ".ssh"):
		return regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
	case strings.Contains(p, ".htpasswd"):
		return regexp.MustCompile(`(?m)^[^:\s]+:\$`)
	case strings.Contains(p, ".aws") || strings.Contains(p, "credentials"):
		return regexp.MustCompile(`aws_access_key_id|aws_secret_access_key`)
	case strings.HasSuffix(p, ".zip"), strings.HasSuffix(p, ".tar"), strings.Contains(p, ".tar."):
		return regexp.MustCompile("^(PK\x03\x04|\x1f\x8b)")
	}
	return nil
}

var verifyClient = &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// verifyFinding dosyayı siteden isteyip içeriğin gerçekten sızan türde olup olmadığına bakar.
// Tek sayfa uygulamaları her yola 200 ile ana sayfayı döndürdüğünden durum kodu tek başına yeterli değil.
func verifyFinding(f Finding, hosts []string) Finding {
	sig := signature(f.Path)
	if sig == nil {
		f.Note = "içerik kontrolü yapılamadı; elle kontrol et"
		return f
	}
	cand := hosts
	if f.Site != "" {
		cand = []string{f.Site}
	}
	if len(cand) > 10 {
		cand = cand[:10]
	}
	for _, h := range cand {
		for _, scheme := range []string{"https", "http"} {
			u := scheme + "://" + h + f.Path
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
			req.Header.Set("User-Agent", "Kokpit-Guvenlik-Kontrolu")
			resp, err := verifyClient.Do(req)
			if err != nil {
				cancel()
				continue
			}
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
			cancel()
			if resp.StatusCode == 200 && sig.Match(body) {
				f.Confirmed, f.URL = true, u
				f.Note = "dosya dışarıdan okunabiliyor"
				return f
			}
			break // https yanıt verdiyse http'yi deneme
		}
	}
	f.Note = "200 döndü ama içerik gerçek dosyaya benzemiyor (muhtemelen sitenin kendi sayfası)"
	return f
}

// ---------- yardımcı: IP doğrulama ----------

func ValidIP(s string) bool { return net.ParseIP(s) != nil }
