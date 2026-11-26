// Package sitewizard yeni bir siteyi tek seferde yayına alır: Nginx site dosyası,
// Cloudflare Tünel yönlendirmesi, DNS kaydı ve uptime izleyicisi. Adımlardan biri
// başarısız olursa o ana kadar yapılanlar geri alınır.
package sitewizard

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OsmanxCan/kokpit/internal/nginx"
	"github.com/OsmanxCan/kokpit/internal/tunnel"
)

type Request struct {
	Host   string `json:"host"`
	Kind   string `json:"kind"` // proxy | static
	Port   int    `json:"port"`
	Root   string `json:"root"`
	SPA    bool   `json:"spa"`    // statik: bulunamayan yollar index.html'e gitsin
	BodyMB int    `json:"bodyMb"` // en büyük istek gövdesi (yükleme)
	Tunnel bool   `json:"tunnel"` // tünel kuralı ekle
	DNS    bool   `json:"dns"`    // DNS kaydı oluştur
	Uptime bool   `json:"uptime"` // uptime izleyicisi ekle
}

// Site sihirbazla oluşturulmuş bir sitenin kaydı (kaldırırken geri almak için).
type Site struct {
	Host      string `json:"host"`
	Kind      string `json:"kind"`
	Port      int    `json:"port,omitempty"`
	Root      string `json:"root,omitempty"`
	Nginx     string `json:"nginx"` // sites-available dosyası
	Tunnel    bool   `json:"tunnel"`
	DNSZone   string `json:"dnsZone,omitempty"`
	DNSRecord string `json:"dnsRecord,omitempty"`
	DNSMethod string `json:"dnsMethod,omitempty"` // cloudflare | cloudflared | manual | ""
	Monitor   string `json:"monitor,omitempty"`
	CreatedAt int64  `json:"createdAt"`
}

type Step struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok | skipped | failed | rolledback | info
	Detail string `json:"detail"`
}

// Deps sihirbazın dış bağımlılıkları (test ve paket bağımlılığı için arayüz).
type Deps struct {
	CFEnabled    func() bool
	EnsureCNAME  func(host, target, comment string) (zone, record string, created bool, err error)
	DeleteRecord func(zone, record string) error
	AddMonitor   func(name, url string) (string, error)
	DelMonitor   func(id string) error
}

type Wizard struct {
	mu   sync.Mutex
	path string
	deps Deps

	SitesAvailable string
	SitesEnabled   string
	LogDir         string
}

func New(dataDir string, d Deps) *Wizard {
	return &Wizard{path: filepath.Join(dataDir, "sihirbaz-siteleri.json"), deps: d,
		SitesAvailable: "/etc/nginx/sites-available", SitesEnabled: nginx.SitesDir, LogDir: "/var/log/nginx"}
}

func (w *Wizard) load() []Site {
	var l []Site
	if b, err := os.ReadFile(w.path); err == nil {
		json.Unmarshal(b, &l)
	}
	return l
}

func (w *Wizard) save(l []Site) {
	b, _ := json.MarshalIndent(l, "", "  ")
	os.WriteFile(w.path, b, 0o600)
}

func (w *Wizard) Sites() []Site {
	w.mu.Lock()
	defer w.mu.Unlock()
	l := w.load()
	if l == nil {
		l = []Site{}
	}
	return l
}

// ---------- doğrulama ve önizleme ----------

// safePath Nginx ayarına yazılacak klasör yolu: boşluk, ; { } " gibi karakterler ayar enjeksiyonuna yol açar.
var safePath = regexp.MustCompile(`^/[A-Za-z0-9._/@+-]*$`)

func (w *Wizard) validate(r *Request) error {
	r.Host = strings.ToLower(strings.TrimSpace(r.Host))
	r.Host = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(r.Host, "https://"), "http://"), "/")
	if !tunnel.ValidHost(r.Host) {
		return errors.New("geçerli bir alan adı gir (örn. uygulama.example.com)")
	}
	switch r.Kind {
	case "proxy":
		if r.Port < 1 || r.Port > 65535 {
			return errors.New("port 1 ile 65535 arasında olmalı")
		}
		if r.Port == 80 || r.Port == 443 {
			return errors.New("80 ve 443 Nginx'in kendi portları; uygulamanın dinlediği portu gir")
		}
	case "static":
		r.Root = filepath.Clean(strings.TrimSpace(r.Root))
		if !safePath.MatchString(r.Root) {
			return errors.New("klasör yolunda boşluk veya özel karakter olmamalı")
		}
		if !filepath.IsAbs(r.Root) || r.Root == "/" || strings.HasPrefix(r.Root, "/etc") || strings.HasPrefix(r.Root, "/root") ||
			strings.HasPrefix(r.Root, "/proc") || strings.HasPrefix(r.Root, "/sys") {
			return errors.New("geçerli bir site klasörü gir (örn. /var/www/site/dist)")
		}
		if fi, err := os.Stat(r.Root); err != nil || !fi.IsDir() {
			return errors.New(r.Root + " bulunamadı veya bir klasör değil")
		}
	default:
		return errors.New("site türü proxy veya static olmalı")
	}
	if r.BodyMB == 0 {
		r.BodyMB = 50
	}
	if r.BodyMB < 1 || r.BodyMB > 10240 {
		return errors.New("en büyük istek boyutu 1 MB ile 10 GB arasında olmalı")
	}
	// çakışmalar
	if sites, err := nginx.SitesIn(w.SitesEnabled); err == nil {
		for _, s := range sites {
			for _, n := range s.Names {
				if strings.EqualFold(n, r.Host) {
					return fmt.Errorf("%s zaten Nginx'te tanımlı (%s)", r.Host, s.File)
				}
			}
		}
	}
	if _, err := os.Stat(w.nginxFile(r.Host)); err == nil {
		return fmt.Errorf("%s zaten var", w.nginxFile(r.Host))
	}
	if r.Tunnel {
		p, ok := tunnel.Available()
		if !ok {
			return errors.New("sunucuda yerel olarak yönetilen bir Cloudflare Tünel bulunamadı (" + p + "); tünel seçeneğini kapat")
		}
		c, err := tunnel.Load(p)
		if err != nil {
			return err
		}
		if c.Has(r.Host) {
			return fmt.Errorf("%s için tünelde zaten bir kural var", r.Host)
		}
	}
	return nil
}

func (w *Wizard) nginxFile(host string) string { return filepath.Join(w.SitesAvailable, host+".conf") }

// NginxConfig site dosyasının içeriği.
func (w *Wizard) NginxConfig(r Request) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Kokpit yeni site sihirbazı ile oluşturuldu (%s).\n", time.Now().Format("2006-01-02"))
	b.WriteString("server {\n    listen 80;\n    listen [::]:80;\n")
	fmt.Fprintf(&b, "    server_name %s;\n\n", r.Host)
	fmt.Fprintf(&b, "    access_log %s/%s.access.log;\n", w.LogDir, r.Host)
	fmt.Fprintf(&b, "    error_log %s/%s.error.log;\n", w.LogDir, r.Host)
	fmt.Fprintf(&b, "    client_max_body_size %dm;\n\n", r.BodyMB)
	b.WriteString("    # .env, .git gibi gizli dosyalar dışarıdan okunamasın (.well-known hariç)\n")
	b.WriteString("    location ~ /\\.(?!well-known/) {\n        deny all;\n        return 404;\n    }\n\n")
	if r.Kind == "proxy" {
		b.WriteString("    location / {\n")
		fmt.Fprintf(&b, "        proxy_pass http://127.0.0.1:%d;\n", r.Port)
		b.WriteString("        proxy_http_version 1.1;\n")
		b.WriteString("        proxy_set_header Host $host;\n")
		b.WriteString("        proxy_set_header X-Real-IP $remote_addr;\n")
		b.WriteString("        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
		b.WriteString("        proxy_set_header X-Forwarded-Proto https;\n")
		b.WriteString("        proxy_set_header Upgrade $http_upgrade;\n")
		b.WriteString("        proxy_set_header Connection \"upgrade\";\n")
		b.WriteString("        proxy_read_timeout 120s;\n")
		b.WriteString("    }\n")
	} else {
		fmt.Fprintf(&b, "    root %s;\n    index index.html index.htm;\n\n", r.Root)
		b.WriteString("    location / {\n")
		if r.SPA {
			b.WriteString("        try_files $uri $uri/ /index.html;\n")
		} else {
			b.WriteString("        try_files $uri $uri/ =404;\n")
		}
		b.WriteString("    }\n")
	}
	b.WriteString("}\n")
	return b.String()
}

type Preview struct {
	Host     string   `json:"host"`
	Nginx    string   `json:"nginx"`
	NginxAt  string   `json:"nginxAt"`
	Tunnel   string   `json:"tunnel"` // eklenecek kural (boş = yok)
	TunnelID string   `json:"tunnelId"`
	DNS      string   `json:"dns"`     // yapılacak DNS işlemi açıklaması
	DNSMode  string   `json:"dnsMode"` // cloudflare | cloudflared | manual | none
	Warnings []string `json:"warnings"`
}

// cloudflaredCert tünel oluştururken alınan hesap sertifikası (DNS yönlendirmesi için gerekir).
func cloudflaredCert() bool {
	for _, p := range []string{"/root/.cloudflared/cert.pem", "/etc/cloudflared/cert.pem"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

func (w *Wizard) dnsMode(r Request) string {
	switch {
	case !r.DNS:
		return "none"
	case !r.Tunnel:
		return "manual"
	case w.deps.CFEnabled != nil && w.deps.CFEnabled():
		return "cloudflare"
	case cloudflaredCert():
		return "cloudflared"
	}
	return "manual"
}

func listening(port int) bool {
	c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// Preview yapılacakları gösterir; hiçbir şey değiştirmez.
func (w *Wizard) Preview(r Request) (Preview, error) {
	if err := w.validate(&r); err != nil {
		return Preview{}, err
	}
	p := Preview{Host: r.Host, Nginx: w.NginxConfig(r), NginxAt: w.nginxFile(r.Host), DNSMode: w.dnsMode(r), Warnings: []string{}}
	if r.Kind == "proxy" && !listening(r.Port) {
		p.Warnings = append(p.Warnings, fmt.Sprintf("Şu an 127.0.0.1:%d portunda çalışan bir uygulama yok. Site, uygulama başlatılınca açılır.", r.Port))
	}
	if r.Tunnel {
		path, _ := tunnel.Available()
		c, _ := tunnel.Load(path)
		if c != nil {
			p.TunnelID = c.TunnelID
		}
		p.Tunnel = fmt.Sprintf("%s içine:\n  - hostname: %s\n    service: http://localhost:80", path, r.Host)
		p.Warnings = append(p.Warnings, "Tünel yeniden başlatılacak; tüneldeki tüm sitelerde birkaç saniyelik kesinti olur.")
	}
	target := p.TunnelID + ".cfargotunnel.com"
	switch p.DNSMode {
	case "cloudflare":
		p.DNS = fmt.Sprintf("Cloudflare API ile %s → %s CNAME kaydı (proxy açık)", r.Host, target)
	case "cloudflared":
		p.DNS = fmt.Sprintf("cloudflared tunnel route dns ile %s kaydı", r.Host)
	case "manual":
		if r.Tunnel {
			p.DNS = fmt.Sprintf("DNS otomatik eklenemiyor (Cloudflare token veya cloudflared sertifikası yok). Cloudflare panelinde şu kaydı ekle: CNAME %s → %s (proxy açık)", r.Host, target)
		} else {
			p.DNS = fmt.Sprintf("Tünel kullanılmadığı için DNS'te %s alan adını sunucunun IP adresine yönlendirmen gerekiyor.", r.Host)
		}
	}
	return p, nil
}

// ---------- oluşturma ----------

func run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func lastLine(s string) string {
	l := strings.Split(strings.TrimSpace(s), "\n")
	return l[len(l)-1]
}

// Create siteyi oluşturur. Her adımın sonucunu döner; zorunlu bir adım başarısız olursa
// önceki adımlar geri alınır.
func (w *Wizard) Create(r Request) ([]Step, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.validate(&r); err != nil {
		return nil, err
	}
	var steps []Step
	var undo []func() string
	site := Site{Host: r.Host, Kind: r.Kind, Port: r.Port, Root: r.Root, CreatedAt: time.Now().Unix()}
	fail := func(name string, err error) ([]Step, error) {
		steps = append(steps, Step{Name: name, Status: "failed", Detail: err.Error()})
		for i := len(undo) - 1; i >= 0; i-- {
			if msg := undo[i](); msg != "" {
				steps = append(steps, Step{Name: "geri alındı", Status: "rolledback", Detail: msg})
			}
		}
		return steps, fmt.Errorf("%s: %v", name, err)
	}

	// 1) Nginx
	file := w.nginxFile(r.Host)
	link := filepath.Join(w.SitesEnabled, r.Host+".conf")
	os.MkdirAll(w.SitesAvailable, 0o755)
	if err := os.WriteFile(file, []byte(w.NginxConfig(r)), 0o644); err != nil {
		return fail("Nginx", err)
	}
	if err := os.Symlink(file, link); err != nil {
		os.Remove(file)
		return fail("Nginx", err)
	}
	undo = append(undo, func() string {
		os.Remove(link)
		os.Remove(file)
		run("systemctl", "reload", "nginx")
		return "Nginx site dosyası silindi"
	})
	if out, err := run("nginx", "-t"); err != nil {
		return fail("Nginx", fmt.Errorf("ayar doğrulanamadı: %s", lastLine(out)))
	}
	if out, err := run("systemctl", "reload", "nginx"); err != nil {
		return fail("Nginx", fmt.Errorf("yeniden yüklenemedi: %s", lastLine(out)))
	}
	site.Nginx = file
	steps = append(steps, Step{Name: "Nginx", Status: "ok", Detail: file + " oluşturuldu, Nginx yeniden yüklendi"})

	// 2) Tünel
	tunnelID := ""
	if r.Tunnel {
		path, _ := tunnel.Available()
		c, err := tunnel.Load(path)
		if err != nil {
			return fail("Tünel", err)
		}
		tunnelID = c.TunnelID
		content, err := c.WithRule(r.Host, "http://localhost:80")
		if err != nil {
			return fail("Tünel", err)
		}
		if err := tunnel.Apply(path, content); err != nil {
			return fail("Tünel", err)
		}
		undo = append(undo, func() string {
			if c2, err := tunnel.Load(path); err == nil {
				if back, err := c2.WithoutRule(r.Host); err == nil && tunnel.Apply(path, back) == nil {
					return "tünel kuralı kaldırıldı"
				}
			}
			return "tünel kuralı kaldırılamadı; " + path + " dosyasını kontrol et"
		})
		site.Tunnel = true
		steps = append(steps, Step{Name: "Tünel", Status: "ok", Detail: "kural eklendi, cloudflared yeniden başlatıldı"})
	} else {
		steps = append(steps, Step{Name: "Tünel", Status: "skipped", Detail: "tünel kullanılmıyor"})
	}

	// 3) DNS (başarısız olursa site yine kurulu kalır; elle eklenebilir)
	target := tunnelID + ".cfargotunnel.com"
	switch w.dnsMode(r) {
	case "cloudflare":
		zone, rec, created, err := w.deps.EnsureCNAME(r.Host, target, "Kokpit sihirbazı")
		if err != nil {
			steps = append(steps, Step{Name: "DNS", Status: "failed", Detail: err.Error() + ". Elle ekle: CNAME " + r.Host + " → " + target})
		} else {
			if created {
				site.DNSZone, site.DNSRecord = zone, rec
			}
			site.DNSMethod = "cloudflare"
			steps = append(steps, Step{Name: "DNS", Status: "ok", Detail: "CNAME " + r.Host + " → " + target + map[bool]string{true: " oluşturuldu", false: " zaten vardı"}[created]})
		}
	case "cloudflared":
		if out, err := run("cloudflared", "tunnel", "route", "dns", tunnelID, r.Host); err != nil {
			steps = append(steps, Step{Name: "DNS", Status: "failed", Detail: lastLine(out) + ". Elle ekle: CNAME " + r.Host + " → " + target})
		} else {
			site.DNSMethod = "cloudflared"
			steps = append(steps, Step{Name: "DNS", Status: "ok", Detail: "cloudflared ile DNS kaydı eklendi"})
		}
	case "manual":
		site.DNSMethod = "manual"
		d := "Cloudflare panelinde ekle: CNAME " + r.Host + " → " + target + " (proxy açık)"
		if !r.Tunnel {
			d = "DNS'te " + r.Host + " alan adını sunucunun IP adresine yönlendir"
		}
		steps = append(steps, Step{Name: "DNS", Status: "info", Detail: d})
	default:
		steps = append(steps, Step{Name: "DNS", Status: "skipped", Detail: "seçilmedi"})
	}

	// 4) Uptime
	if r.Uptime && w.deps.AddMonitor != nil {
		id, err := w.deps.AddMonitor(r.Host, "https://"+r.Host)
		if err != nil {
			steps = append(steps, Step{Name: "Uptime", Status: "failed", Detail: err.Error()})
		} else {
			site.Monitor = id
			steps = append(steps, Step{Name: "Uptime", Status: "ok", Detail: "izleyici eklendi"})
		}
	}

	l := w.load()
	w.save(append(l, site))
	return steps, nil
}

// Remove sihirbazla oluşturulmuş siteyi kaldırır: DNS kaydı (Kokpit oluşturduysa), tünel
// kuralı, Nginx dosyası ve uptime izleyicisi. Uygulamanın dosyalarına dokunmaz.
func (w *Wizard) Remove(host string) ([]Step, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	l := w.load()
	idx := -1
	for i, s := range l {
		if s.Host == host {
			idx = i
		}
	}
	if idx < 0 {
		return nil, errors.New("bu site sihirbazla oluşturulmamış; sadece sihirbazın oluşturduğu siteler buradan kaldırılabilir")
	}
	s := l[idx]
	var steps []Step
	if s.DNSRecord != "" && w.deps.DeleteRecord != nil {
		if err := w.deps.DeleteRecord(s.DNSZone, s.DNSRecord); err != nil {
			steps = append(steps, Step{Name: "DNS", Status: "failed", Detail: err.Error()})
		} else {
			steps = append(steps, Step{Name: "DNS", Status: "ok", Detail: "DNS kaydı silindi"})
		}
	} else if s.DNSMethod != "" {
		steps = append(steps, Step{Name: "DNS", Status: "info", Detail: "DNS kaydını Cloudflare panelinden sil"})
	}
	if s.Tunnel {
		path, _ := tunnel.Available()
		c, err := tunnel.Load(path)
		if err == nil && c.Has(host) {
			var back string
			back, err = c.WithoutRule(host)
			if err == nil {
				err = tunnel.Apply(path, back)
			}
		}
		if err != nil {
			steps = append(steps, Step{Name: "Tünel", Status: "failed", Detail: err.Error()})
		} else {
			steps = append(steps, Step{Name: "Tünel", Status: "ok", Detail: "kural kaldırıldı"})
		}
	}
	os.Remove(filepath.Join(w.SitesEnabled, host+".conf"))
	os.Remove(s.Nginx)
	if out, err := run("nginx", "-t"); err != nil {
		steps = append(steps, Step{Name: "Nginx", Status: "failed", Detail: lastLine(out)})
	} else {
		run("systemctl", "reload", "nginx")
		steps = append(steps, Step{Name: "Nginx", Status: "ok", Detail: "site dosyası silindi"})
	}
	if s.Monitor != "" && w.deps.DelMonitor != nil {
		w.deps.DelMonitor(s.Monitor)
		steps = append(steps, Step{Name: "Uptime", Status: "ok", Detail: "izleyici silindi"})
	}
	w.save(append(l[:idx], l[idx+1:]...))
	return steps, nil
}

// ---------- arka plan işleri ----------
// Tünel yeniden başlatılırken panelin kendi bağlantısı da (panel aynı tünelin arkasındaysa)
// birkaç saniye kopar. Bu yüzden oluşturma/kaldırma arka planda çalışır; arayüz sonucu sorar.

type Job struct {
	ID     string `json:"id"`
	Op     string `json:"op"` // create | remove
	Host   string `json:"host"`
	Done   bool   `json:"done"`
	Error  string `json:"error,omitempty"`
	Steps  []Step `json:"steps"`
	Start  int64  `json:"start"`
	finish int64
}

var (
	jobsMu sync.Mutex
	jobs   = map[string]*Job{}
)

func newJobID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (w *Wizard) startJob(op, host string, fn func() ([]Step, error), onDone func(*Job)) string {
	j := &Job{ID: newJobID(), Op: op, Host: host, Steps: []Step{}, Start: time.Now().Unix()}
	jobsMu.Lock()
	for id, o := range jobs { // bir saatten eski bitmiş işleri temizle
		if o.Done && time.Since(time.Unix(o.finish, 0)) > time.Hour {
			delete(jobs, id)
		}
	}
	jobs[j.ID] = j
	jobsMu.Unlock()
	go func() {
		steps, err := fn()
		jobsMu.Lock()
		j.Steps = steps
		if j.Steps == nil {
			j.Steps = []Step{}
		}
		if err != nil {
			j.Error = err.Error()
		}
		j.Done, j.finish = true, time.Now().Unix()
		cp := *j
		jobsMu.Unlock()
		if onDone != nil {
			onDone(&cp)
		}
	}()
	return j.ID
}

// StartCreate isteği doğrular ve siteyi arka planda oluşturur.
func (w *Wizard) StartCreate(r Request, onDone func(*Job)) (string, error) {
	if err := w.validate(&r); err != nil {
		return "", err
	}
	return w.startJob("create", r.Host, func() ([]Step, error) { return w.Create(r) }, onDone), nil
}

// StartRemove siteyi arka planda kaldırır.
func (w *Wizard) StartRemove(host string, onDone func(*Job)) (string, error) {
	found := false
	for _, s := range w.Sites() {
		if s.Host == host {
			found = true
		}
	}
	if !found {
		return "", errors.New("bu site sihirbazla oluşturulmamış; sadece sihirbazın oluşturduğu siteler buradan kaldırılabilir")
	}
	return w.startJob("remove", host, func() ([]Step, error) { return w.Remove(host) }, onDone), nil
}

// GetJob işin anlık durumunu döner.
func GetJob(id string) (Job, bool) {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	j, ok := jobs[id]
	if !ok {
		return Job{}, false
	}
	return *j, true
}
