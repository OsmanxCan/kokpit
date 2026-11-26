// Package tunnel sunucuda yerel olarak yönetilen Cloudflare Tünel'in (config.yml)
// yönlendirme kurallarını (ingress) okur ve değiştirir.
//
// Dosya elle de düzenlenebildiği için tam bir YAML yeniden yazımı yapılmaz; sadece ilgili
// satırlar eklenir/çıkarılır, geri kalan her şey (yorumlar dahil) olduğu gibi kalır.
// Her değişiklikten önce yedek alınır, cloudflared ile doğrulanır; servis yeniden
// başladıktan sonra çalışmazsa yedeğe dönülür.
package tunnel

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const DefaultConfig = "/etc/cloudflared/config.yml"

type Rule struct {
	Hostname string `json:"hostname"`
	Service  string `json:"service"`
	Path     string `json:"path,omitempty"`
}

type Config struct {
	Path     string `json:"path"`
	TunnelID string `json:"tunnelId"`
	Rules    []Rule `json:"rules"`
	raw      string
}

var execStartRe = regexp.MustCompile(`--config[= ]+(\S+)`)

// ConfigPath cloudflared servisinin kullandığı ayar dosyasını bulur.
func ConfigPath() string {
	if out, err := exec.Command("systemctl", "cat", "cloudflared").Output(); err == nil {
		if m := execStartRe.FindStringSubmatch(string(out)); m != nil {
			return m[1]
		}
	}
	return DefaultConfig
}

// Available sunucuda yerel olarak yönetilen bir tünel var mı.
func Available() (string, bool) {
	p := ConfigPath()
	_, err := os.Stat(p)
	return p, err == nil
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := Parse(string(b))
	c.Path = path
	return c, nil
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, " #"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return strings.Trim(s, `"'`)
}

// Parse config.yml'deki tünel kimliğini ve ingress kurallarını okur.
func Parse(src string) *Config {
	c := &Config{raw: src}
	inIngress := false
	var cur *Rule
	flush := func() {
		if cur != nil {
			c.Rules = append(c.Rules, *cur)
			cur = nil
		}
	}
	for _, ln := range strings.Split(src, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		indent := len(ln) - len(strings.TrimLeft(ln, " "))
		if indent == 0 && !strings.HasPrefix(t, "-") {
			flush()
			inIngress = strings.HasPrefix(t, "ingress:")
			if k, v, ok := strings.Cut(t, ":"); ok && k == "tunnel" {
				c.TunnelID = unquote(v)
			}
			continue
		}
		if !inIngress {
			continue
		}
		if strings.HasPrefix(t, "- ") || t == "-" {
			flush()
			cur = &Rule{}
			t = strings.TrimSpace(strings.TrimPrefix(t, "-"))
			if t == "" {
				continue
			}
		}
		if cur == nil {
			continue
		}
		k, v, ok := strings.Cut(t, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "hostname":
			cur.Hostname = unquote(v)
		case "service":
			cur.Service = unquote(v)
		case "path":
			cur.Path = unquote(v)
		}
	}
	flush()
	return c
}

// Has hostname için bir kural var mı.
func (c *Config) Has(host string) bool {
	for _, r := range c.Rules {
		if strings.EqualFold(r.Hostname, host) {
			return true
		}
	}
	return false
}

// itemLines src içindeki ingress maddelerinin satır aralıklarını döner: [başlangıç, bitiş).
func itemLines(lines []string) (items [][2]int, itemIndent string) {
	inIngress := false
	start := -1
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		indent := len(ln) - len(strings.TrimLeft(ln, " "))
		if t != "" && !strings.HasPrefix(t, "#") && indent == 0 && !strings.HasPrefix(t, "-") {
			if start >= 0 {
				items = append(items, [2]int{start, i})
				start = -1
			}
			inIngress = strings.HasPrefix(t, "ingress:")
			continue
		}
		if !inIngress {
			continue
		}
		if strings.HasPrefix(t, "- ") || t == "-" {
			if start >= 0 {
				items = append(items, [2]int{start, i})
			}
			start = i
			if itemIndent == "" {
				itemIndent = ln[:indent]
			}
		}
	}
	if start >= 0 {
		end := len(lines)
		for end > start+1 && strings.TrimSpace(lines[end-1]) == "" {
			end--
		}
		items = append(items, [2]int{start, end})
	}
	return
}

var hostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// ValidHost alan adı biçimi doğru mu.
func ValidHost(h string) bool { return len(h) <= 253 && hostRe.MatchString(h) }

var serviceRe = regexp.MustCompile(`^https?://(localhost|127\.0\.0\.1|\[::1\])(:\d{1,5})?$`)

// WithRule hostname → service kuralı eklenmiş yeni içeriği döner. Kural, en sondaki
// "her şeyi yakala" (hostname'siz) kuralın hemen önüne eklenir.
func (c *Config) WithRule(host, service string) (string, error) {
	if !ValidHost(host) {
		return "", errors.New("geçersiz alan adı")
	}
	if !serviceRe.MatchString(service) {
		return "", errors.New("hizmet adresi http://localhost:PORT biçiminde olmalı")
	}
	if c.Has(host) {
		return "", fmt.Errorf("%s için tünelde zaten bir kural var", host)
	}
	lines := strings.Split(c.raw, "\n")
	items, ind := itemLines(lines)
	if len(items) == 0 || c.Rules[len(c.Rules)-1].Hostname != "" {
		return "", errors.New("tünel ayarında en sonda hostname'siz bir kural (örn. service: http_status:404) bulunamadı")
	}
	last := items[len(items)-1]
	// iç alanların girintisi: maddedeki "- " sonrasıyla hizalı
	inner := ind + "  "
	block := []string{ind + "- hostname: " + host, inner + "service: " + service}
	out := append([]string{}, lines[:last[0]]...)
	out = append(out, block...)
	out = append(out, lines[last[0]:]...)
	return strings.Join(out, "\n"), nil
}

// WithoutRule hostname kuralı çıkarılmış içeriği döner.
func (c *Config) WithoutRule(host string) (string, error) {
	lines := strings.Split(c.raw, "\n")
	items, _ := itemLines(lines)
	if len(items) != len(c.Rules) {
		return "", errors.New("tünel ayarı okunamadı")
	}
	for i, r := range c.Rules {
		if strings.EqualFold(r.Hostname, host) {
			it := items[i]
			out := append([]string{}, lines[:it[0]]...)
			out = append(out, lines[it[1]:]...)
			return strings.Join(out, "\n"), nil
		}
	}
	return "", fmt.Errorf("%s için tünelde kural yok", host)
}

// Validate içeriği cloudflared ile doğrular.
func Validate(content string) error {
	f, err := os.CreateTemp("", "kokpit-tunel-*.yml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	f.WriteString(content)
	f.Close()
	if _, err := exec.LookPath("cloudflared"); err != nil {
		return nil // doğrulama yapılamıyor; Apply'daki çalışma kontrolü yine korur
	}
	out, err := exec.Command("cloudflared", "tunnel", "--config", f.Name(), "ingress", "validate").CombinedOutput()
	if err != nil {
		return fmt.Errorf("cloudflared ayarı geçersiz buldu: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// Apply yeni içeriği yazar ve cloudflared'i yeniden başlatır. Servis çalışmazsa eski
// içeriğe döner. Yeniden başlatma tüneldeki tüm sitelerde birkaç saniyelik kesintiye yol açar.
func Apply(path, content string) error {
	if err := Validate(content); err != nil {
		return err
	}
	old, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fi, _ := os.Stat(path)
	mode := os.FileMode(0o644)
	if fi != nil {
		mode = fi.Mode().Perm()
	}
	os.WriteFile(path+".kokpit-yedek", old, mode)
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		return err
	}
	if err := restart(); err != nil {
		os.WriteFile(path, old, mode)
		restart()
		return fmt.Errorf("cloudflared yeni ayarla çalışmadı, eski ayara dönüldü: %v", err)
	}
	return nil
}

func restart() error {
	if out, err := exec.Command("systemctl", "restart", "cloudflared").CombinedOutput(); err != nil {
		return fmt.Errorf("yeniden başlatılamadı: %s", strings.TrimSpace(string(out)))
	}
	for i := 0; i < 10; i++ {
		time.Sleep(time.Second)
		out, _ := exec.Command("systemctl", "is-active", "cloudflared").Output()
		if strings.TrimSpace(string(out)) == "active" {
			// açılıştan hemen sonra çökmediğinden emin ol
			time.Sleep(2 * time.Second)
			out, _ = exec.Command("systemctl", "is-active", "cloudflared").Output()
			if strings.TrimSpace(string(out)) == "active" {
				return nil
			}
		}
	}
	return errors.New("servis aktif olmadı")
}
