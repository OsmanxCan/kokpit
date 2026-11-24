// Package update GitHub Releases üzerinden yeni sürüm kontrolü ve kendini güncelleme.
//
// Akış: api.github.com/repos/<repo>/releases/latest → sürüm karşılaştır →
// "kokpit_linux_<arch>" dosyasını indir → checksums.txt ile SHA-256 doğrula →
// çalışan binary'nin yerine atomik olarak koy → süreç çıkar, systemd yeniden başlatır.
package update

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Release struct {
	Tag         string `json:"tag"`
	Version     string `json:"version"`
	Name        string `json:"name"`
	Notes       string `json:"notes"`
	URL         string `json:"url"`
	PublishedAt string `json:"publishedAt"`
	binURL      string
	sumURL      string
}

type Status struct {
	Current   string   `json:"current"`
	Repo      string   `json:"repo"`
	Enabled   bool     `json:"enabled"`
	Latest    *Release `json:"latest"`
	Available bool     `json:"available"`
	CheckedAt int64    `json:"checkedAt"`
	Error     string   `json:"error,omitempty"`
	Arch      string   `json:"arch"`
	CanApply  bool     `json:"canApply"` // binary'nin bulunduğu klasöre yazılabiliyor mu
}

type Checker struct {
	Current string
	Repo    string
	Enabled bool
	OnNew   func(r *Release) // yeni sürüm ilk bulunduğunda (bildirim için)
	APIBase string           // testler için değiştirilebilir
	ExePath string           // testler için; boşsa çalışan binary

	mu       sync.Mutex
	latest   *Release
	checked  time.Time
	err      string
	notified string
	client   *http.Client
}

func New(current, repo string, enabled bool) *Checker {
	return &Checker{Current: current, Repo: repo, Enabled: enabled, APIBase: "https://api.github.com",
		client: &http.Client{Timeout: 30 * time.Second}}
}

// Start 6 saatte bir sürüm kontrolü yapar.
func (c *Checker) Start() {
	if c.Repo == "" {
		return
	}
	go func() {
		time.Sleep(30 * time.Second)
		for {
			if c.IsEnabled() {
				c.Check()
			}
			time.Sleep(6 * time.Hour)
		}
	}()
}

// SetEnabled otomatik kontrolü açar/kapatır (ayarlar sayfasından).
func (c *Checker) SetEnabled(v bool) {
	c.mu.Lock()
	c.Enabled = v
	c.mu.Unlock()
}

func (c *Checker) IsEnabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Enabled
}

func (c *Checker) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Status{Current: c.Current, Repo: c.Repo, Enabled: c.Enabled, Latest: c.latest, Error: c.err,
		Arch: runtime.GOOS + "/" + runtime.GOARCH, CanApply: canWriteExe()}
	if !c.checked.IsZero() {
		s.CheckedAt = c.checked.Unix()
	}
	s.Available = c.latest != nil && Newer(c.latest.Version, c.Current)
	return s
}

func assetName() string { return fmt.Sprintf("kokpit_%s_%s", runtime.GOOS, runtime.GOARCH) }

// Check GitHub'dan son sürümü çeker.
func (c *Checker) Check() (*Release, error) {
	r, err := c.fetchLatest()
	c.mu.Lock()
	c.checked = time.Now()
	if err != nil {
		c.err = err.Error()
		c.mu.Unlock()
		return nil, err
	}
	c.err = ""
	c.latest = r
	notify := Newer(r.Version, c.Current) && c.notified != r.Version
	if notify {
		c.notified = r.Version
	}
	c.mu.Unlock()
	if notify && c.OnNew != nil {
		c.OnNew(r)
	}
	return r, nil
}

func (c *Checker) fetchLatest() (*Release, error) {
	if c.Repo == "" || !strings.Contains(c.Repo, "/") {
		return nil, errors.New("GitHub deposu ayarlı değil (PANEL_REPO)")
	}
	req, _ := http.NewRequest("GET", c.APIBase+"/repos/"+c.Repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "kokpit/"+c.Current)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub'a ulaşılamadı: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, errors.New("depoda henüz yayınlanmış sürüm yok")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub yanıtı: %s", resp.Status)
	}
	var gr struct {
		TagName     string `json:"tag_name"`
		Name        string `json:"name"`
		Body        string `json:"body"`
		HTMLURL     string `json:"html_url"`
		PublishedAt string `json:"published_at"`
		Draft       bool   `json:"draft"`
		Prerelease  bool   `json:"prerelease"`
		Assets      []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&gr); err != nil {
		return nil, err
	}
	r := &Release{Tag: gr.TagName, Version: strings.TrimPrefix(gr.TagName, "v"), Name: gr.Name,
		Notes: gr.Body, URL: gr.HTMLURL, PublishedAt: gr.PublishedAt}
	if len(r.Notes) > 8000 {
		r.Notes = r.Notes[:8000] + "\n…"
	}
	for _, a := range gr.Assets {
		switch a.Name {
		case assetName():
			r.binURL = a.URL
		case "checksums.txt":
			r.sumURL = a.URL
		}
	}
	return r, nil
}

// Newer a sürümü b'den yeni mi? ("1.2.10" > "1.2.9"). "dev" her zaman en eski sayılır.
func Newer(a, b string) bool {
	pa, pb := parse(a), parse(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parse(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		n, err := strconv.Atoi(p)
		if err != nil {
			return [3]int{-1, -1, -1}
		}
		out[i] = n
	}
	return out
}

func exePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}

func canWriteExe() bool {
	p, err := exePath()
	if err != nil {
		return false
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".kokpit-yazma-testi-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

func (c *Checker) download(url string, w io.Writer, max int64) error {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "kokpit/"+c.Current)
	cl := &http.Client{Timeout: 5 * time.Minute}
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("indirme hatası: %s", resp.Status)
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, max+1))
	if err != nil {
		return err
	}
	if n > max {
		return errors.New("dosya beklenenden büyük")
	}
	return nil
}

// Apply son sürümü indirir, doğrular ve çalışan binary'nin yerine koyar.
// Başarılı olursa yeni sürüm numarasını döner; çağıran taraf süreci yeniden başlatmalıdır.
func (c *Checker) Apply() (string, error) {
	r, err := c.Check()
	if err != nil {
		return "", err
	}
	if !Newer(r.Version, c.Current) {
		return "", fmt.Errorf("zaten en güncel sürüm (%s)", c.Current)
	}
	if r.binURL == "" {
		return "", fmt.Errorf("bu sürümde %s dosyası yok", assetName())
	}
	if r.sumURL == "" {
		return "", errors.New("bu sürümde checksums.txt yok, doğrulama yapılamaz")
	}

	var sums strings.Builder
	if err := c.download(r.sumURL, &sums, 64<<10); err != nil {
		return "", fmt.Errorf("checksums.txt: %v", err)
	}
	want := ""
	sc := bufio.NewScanner(strings.NewReader(sums.String()))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == assetName() {
			want = strings.ToLower(f[0])
		}
	}
	if len(want) != 64 {
		return "", errors.New("checksums.txt içinde bu dosyanın özeti yok")
	}

	exe := c.ExePath
	if exe == "" {
		if exe, err = exePath(); err != nil {
			return "", err
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".kokpit-yeni-*")
	if err != nil {
		return "", fmt.Errorf("geçici dosya oluşturulamadı: %v", err)
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	if err := c.download(r.binURL, io.MultiWriter(tmp, h), 200<<20); err != nil {
		tmp.Close()
		return "", err
	}
	tmp.Close()
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return "", fmt.Errorf("SHA-256 doğrulaması başarısız (beklenen %s…, gelen %s…)", want[:12], got[:12])
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return "", err
	}
	// Eski sürümü yedekle (geri dönmek gerekirse)
	_ = copyFile(exe, exe+".eski")
	if err := os.Rename(tmp.Name(), exe); err != nil {
		return "", fmt.Errorf("binary değiştirilemedi: %v", err)
	}
	return r.Version, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
