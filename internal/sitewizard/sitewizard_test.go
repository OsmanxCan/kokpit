package sitewizard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sahte nginx ve systemctl: nginx -t, NGINX_FAIL dosyası varsa başarısız olur
func fakeBin(t *testing.T) string {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "nginx"), []byte("#!/bin/sh\n[ -f \"$NGINX_FAIL\" ] && { echo 'nginx: [emerg] unknown directive'; exit 1; }\necho ok\n"), 0o755)
	os.WriteFile(filepath.Join(dir, "systemctl"), []byte("#!/bin/sh\necho active\n"), 0o755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return dir
}

func newWiz(t *testing.T) *Wizard {
	root := t.TempDir()
	w := New(t.TempDir(), Deps{AddMonitor: func(n, u string) (string, error) { return "m1", nil }, DelMonitor: func(string) error { return nil }})
	w.SitesAvailable = filepath.Join(root, "available")
	w.SitesEnabled = filepath.Join(root, "enabled")
	os.MkdirAll(w.SitesEnabled, 0o755)
	return w
}

func TestCreateRemove(t *testing.T) {
	fakeBin(t)
	w := newWiz(t)
	r := Request{Host: "https://Yeni.Example.com/", Kind: "proxy", Port: 3005, DNS: true, Uptime: true}
	p, err := w.Preview(r)
	if err != nil {
		t.Fatal(err)
	}
	if p.Host != "yeni.example.com" || !strings.Contains(p.Nginx, "proxy_pass http://127.0.0.1:3005;") || !strings.Contains(p.Nginx, `location ~ /\.(?!well-known/)`) ||
		!strings.Contains(p.Nginx, "access_log /var/log/nginx/yeni.example.com.access.log;") || p.DNSMode != "manual" {
		t.Fatalf("önizleme: %+v", p)
	}
	steps, err := w.Create(r)
	if err != nil {
		t.Fatal(err, steps)
	}
	if _, err := os.Lstat(filepath.Join(w.SitesEnabled, "yeni.example.com.conf")); err != nil {
		t.Fatal("bağlantı oluşturulmadı")
	}
	if len(w.Sites()) != 1 || w.Sites()[0].Monitor != "m1" {
		t.Fatalf("kayıt: %+v", w.Sites())
	}
	if _, err := w.Create(r); err == nil {
		t.Fatal("aynı site iki kez oluşturuldu")
	}
	if _, err := w.Remove("yeni.example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(w.SitesAvailable, "yeni.example.com.conf")); err == nil || len(w.Sites()) != 0 {
		t.Fatal("kaldırılmadı")
	}
}

func TestRollback(t *testing.T) {
	bin := fakeBin(t)
	flag := filepath.Join(bin, "fail")
	os.WriteFile(flag, nil, 0o644)
	t.Setenv("NGINX_FAIL", flag)
	w := newWiz(t)
	steps, err := w.Create(Request{Host: "bozuk.example.com", Kind: "proxy", Port: 3006})
	if err == nil {
		t.Fatal("hata beklenirdi")
	}
	if _, err := os.Stat(filepath.Join(w.SitesAvailable, "bozuk.example.com.conf")); err == nil {
		t.Fatal("geri alınmadı")
	}
	if steps[len(steps)-1].Status != "rolledback" || len(w.Sites()) != 0 {
		t.Fatalf("%+v", steps)
	}
}

func TestValidate(t *testing.T) {
	fakeBin(t)
	w := newWiz(t)
	for _, r := range []Request{
		{Host: "localhost", Kind: "proxy", Port: 3000},
		{Host: "a.example.com", Kind: "proxy", Port: 80},
		{Host: "a.example.com", Kind: "static", Root: "/etc"},
		{Host: "a.example.com", Kind: "static", Root: "/yok/boyle/klasor"},
		{Host: "a.example.com;rm", Kind: "proxy", Port: 3000},
		{Host: "a.example.com", Kind: "php", Port: 3000},
		{Host: "a.example.com", Kind: "static", Root: "/tmp; include /etc/passwd"},
	} {
		if _, err := w.Preview(r); err == nil {
			t.Errorf("kabul edildi: %+v", r)
		}
	}
	d := t.TempDir()
	p, err := w.Preview(Request{Host: "statik.example.com", Kind: "static", Root: d, SPA: true})
	if err != nil || !strings.Contains(p.Nginx, "root "+d+";") || !strings.Contains(p.Nginx, "/index.html;") {
		t.Fatalf("statik: %v\n%s", err, p.Nginx)
	}
}
