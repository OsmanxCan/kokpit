// Package weblog Nginx access loglarını analiz eder ve web saldırılarını bulur.
package weblog

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/OsmanxCan/kokpit/internal/cfips"
)

// RealIPFile Kokpit'in yazdığı Nginx ayar dosyası.
const RealIPFile = "/etc/nginx/conf.d/kokpit-realip.conf"

type RealIP struct {
	Nginx      bool     `json:"nginx"`      // nginx kurulu mu
	Configured bool     `json:"configured"` // gerçek IP ayarı var mı
	Header     string   `json:"header"`
	From       int      `json:"from"`       // güvenilen kaynak sayısı
	Kokpit     bool     `json:"kokpit"`     // ayar Kokpit tarafından mı yapıldı
	Tunnel     bool     `json:"tunnel"`     // cloudflared kurulu
	LocalShare float64  `json:"localShare"` // son isteklerin yüzde kaçı 127.0.0.1'den görünüyor
	ConfD      bool     `json:"confd"`      // conf.d nginx.conf'a dahil mi
	Problems   []string `json:"problems,omitempty"`
}

var (
	realHeaderRe = regexp.MustCompile(`(?m)^\s*real_ip_header\s+([^;]+);`)
	realFromRe   = regexp.MustCompile(`(?m)^\s*set_real_ip_from\s+([^;]+);`)
	confdRe      = regexp.MustCompile(`(?m)^\s*include\s+/etc/nginx/conf\.d/\*\.conf\s*;`)
)

func nginxDump() (string, error) {
	if _, err := exec.LookPath("nginx"); err != nil {
		return "", errors.New("nginx kurulu değil")
	}
	out, err := exec.Command("nginx", "-T").CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("nginx -T: %s", lastLine(string(out)))
	}
	return string(out), nil
}

func lastLine(s string) string {
	l := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(l[len(l)-1])
}

// CheckRealIP Nginx'in ziyaretçinin gerçek IP'sini kaydedip kaydetmediğine bakar.
func CheckRealIP() RealIP {
	r := RealIP{}
	if _, err := exec.LookPath("cloudflared"); err == nil {
		r.Tunnel = true
	} else if _, err := os.Stat("/etc/cloudflared"); err == nil {
		r.Tunnel = true
	}
	dump, err := nginxDump()
	if err != nil {
		if strings.Contains(err.Error(), "kurulu değil") {
			return r
		}
		r.Nginx = true
		r.Problems = append(r.Problems, err.Error())
		return r
	}
	r.Nginx = true
	r.ConfD = confdRe.MatchString(dump)
	if m := realHeaderRe.FindAllStringSubmatch(dump, -1); len(m) > 0 {
		r.Header = strings.TrimSpace(m[len(m)-1][1])
		r.Configured = true
	}
	r.From = len(realFromRe.FindAllString(dump, -1))
	if r.Configured && r.From == 0 {
		r.Configured = false
		r.Problems = append(r.Problems, "real_ip_header var ama set_real_ip_from yok; Nginx başlığı kullanmaz")
	}
	_, ferr := os.Stat(RealIPFile)
	r.Kokpit = ferr == nil
	r.LocalShare = localShare("/var/log/nginx/access.log", 2000)
	return r
}

// localShare logun son n satırında 127.0.0.1 / ::1 kaynaklı isteklerin oranı.
func localShare(path string, n int) float64 {
	lines := tail(path, n)
	if len(lines) == 0 {
		return 0
	}
	local := 0
	for _, l := range lines {
		ip, _, _ := strings.Cut(l, " ")
		if ip == "127.0.0.1" || ip == "::1" {
			local++
		}
	}
	return float64(local) / float64(len(lines)) * 100
}

// tail dosyanın son n satırını okur (en fazla 4 MB).
func tail(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() > 4<<20 {
		f.Seek(-4<<20, 2)
	}
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > n*2 {
			lines = lines[len(lines)-n:]
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// RealIPConfig yazılacak ayar dosyasının içeriği.
func RealIPConfig() string {
	var b strings.Builder
	b.WriteString("# Kokpit tarafından oluşturuldu. Ziyaretçinin gerçek IP'si Cloudflare'in\n")
	b.WriteString("# CF-Connecting-IP başlığından alınır. Başlığa sadece aşağıdaki kaynaklardan\n")
	b.WriteString("# (Cloudflare Tünel = 127.0.0.1 ve Cloudflare sunucuları) gelen isteklerde güvenilir;\n")
	b.WriteString("# doğrudan gelen biri başlığı taklit ederek IP'sini gizleyemez.\n")
	b.WriteString("set_real_ip_from 127.0.0.1;\nset_real_ip_from ::1;\n")
	for _, r := range cfips.Ranges {
		b.WriteString("set_real_ip_from " + r + ";\n")
	}
	b.WriteString("real_ip_header CF-Connecting-IP;\nreal_ip_recursive on;\n")
	return b.String()
}

// ApplyRealIP ayar dosyasını yazar, nginx -t ile doğrular ve Nginx'i yeniden yükler.
// Doğrulama başarısız olursa dosyayı geri alır.
func ApplyRealIP() (string, error) {
	st := CheckRealIP()
	if !st.Nginx {
		return "", errors.New("nginx kurulu değil")
	}
	if st.Configured && !st.Kokpit {
		return "", errors.New("Nginx'te zaten bir gerçek IP ayarı var (real_ip_header " + st.Header + "); üzerine yazılmadı")
	}
	if !st.ConfD {
		return "", errors.New("nginx.conf, /etc/nginx/conf.d/*.conf dosyalarını içermiyor; http bloğuna 'include /etc/nginx/conf.d/*.conf;' eklenmeli")
	}
	old, errOld := os.ReadFile(RealIPFile)
	if err := os.WriteFile(RealIPFile, []byte(RealIPConfig()), 0o644); err != nil {
		return "", err
	}
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		if errOld == nil { // önceki Kokpit dosyasını geri koy
			os.WriteFile(RealIPFile, old, 0o644)
		} else {
			os.Remove(RealIPFile)
		}
		return string(out), fmt.Errorf("nginx ayarı doğrulanamadı, değişiklik geri alındı: %s", lastLine(string(out)))
	}
	out, err := exec.Command("systemctl", "reload", "nginx").CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("nginx yeniden yüklenemedi: %s", lastLine(string(out)))
	}
	return "", nil
}

// RemoveRealIP Kokpit'in eklediği ayarı kaldırır.
func RemoveRealIP() error {
	if err := os.Remove(RealIPFile); err != nil {
		return err
	}
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		return fmt.Errorf("nginx -t: %s", lastLine(string(out)))
	}
	return exec.Command("systemctl", "reload", "nginx").Run()
}
