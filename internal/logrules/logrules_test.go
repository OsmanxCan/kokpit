package logrules

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/OsmanxCan/kokpit/internal/settings"
)

func TestCompile(t *testing.T) {
	cases := []struct {
		pattern string
		regex   bool
		line    string
		ip      string
	}{
		{"login failed ip=<IP>", false, `[Nest] 12 - 26.11.2026 WARN login failed ip=45.148.10.5 user=a`, "45.148.10.5"},
		{"Invalid credentials * from <IP>", false, `Invalid credentials for admin@x.com from 2001:db8::7`, "2001:db8::7"},
		{`"ip":"<IP>".*"event":"login_failed"`, true, `{"ip":"185.224.128.9","event":"login_failed"}`, "185.224.128.9"},
		{"login failed ip=<IP>", false, `login ok ip=45.148.10.5`, ""},
		{"login failed ip=<IP>", false, `login failed ip=999.1.1.1`, ""}, // geçersiz IP
		{"a (b) [c] ip=<IP>", false, `a (b) [c] ip=1.2.3.4`, "1.2.3.4"},  // özel karakterler harfiyen
	}
	for _, c := range cases {
		re, err := Compile(settings.LogRule{Pattern: c.pattern, Regex: c.regex})
		if err != nil {
			t.Fatalf("%s: %v", c.pattern, err)
		}
		if got := match(re, c.line); got != c.ip {
			t.Errorf("%q / %q → %q, beklenen %q", c.pattern, c.line, got, c.ip)
		}
	}
	for _, bad := range []string{"ip yok", "<IP> ve <IP>", ""} {
		if _, err := Compile(settings.LogRule{Pattern: bad}); err == nil {
			t.Errorf("kabul edildi: %q", bad)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := settings.LogRule{Name: "Accounts", Source: "journal", Path: "accounts.service", Pattern: "fail <IP>"}
	if err := Validate(&ok); err != nil || ok.Threshold != 10 || ok.Window != 5 || ok.Action != "auto" {
		t.Fatalf("%v %+v", err, ok)
	}
	for _, r := range []settings.LogRule{
		{Name: "x", Source: "file", Path: "goreli.log", Pattern: "<IP>"},
		{Name: "x", Source: "journal", Path: "-u; rm", Pattern: "<IP>"},
		{Name: "x", Source: "file", Path: "/proc/self/environ", Pattern: "<IP>"},
		{Name: "x", Source: "ssh", Path: "/a", Pattern: "<IP>"},
	} {
		if Validate(&r) == nil {
			t.Errorf("kabul edildi: %+v", r)
		}
	}
}

func TestTestAndFollow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	var lines string
	for i := 0; i < 12; i++ {
		lines += fmt.Sprintf("WARN login failed ip=45.148.10.5 try=%d\n", i)
	}
	lines += "INFO login ok ip=203.0.113.9\nWARN login failed ip=193.32.162.79\n"
	os.WriteFile(path, []byte(lines), 0o644)
	r := settings.LogRule{ID: "r1", Name: "Accounts", Source: "file", Path: path, Pattern: "login failed ip=<IP>", Threshold: 10, Window: 5, Enabled: true, BanHours: 1}
	res, err := Test(r, nil)
	if err != nil || res.Matches != 13 || res.IPs["45.148.10.5"] != 12 || res.WouldBan != 1 {
		t.Fatalf("test: %v %+v", err, res)
	}
	r.Pattern = "bulunmayan <IP>"
	res, _ = Test(r, nil)
	if res.Matches != 0 || len(res.Unmatched) == 0 {
		t.Fatalf("eşleşmeyen örnekler: %+v", res)
	}

	if _, err := exec.LookPath("tail"); err != nil {
		t.Skip("tail yok")
	}
	r.Pattern = "login failed ip=<IP>"
	var mu sync.Mutex
	var banned []string
	e := New(func() []settings.LogRule { return []settings.LogRule{r} })
	e.Ban = func(ip, reason string, hours int, action string) (string, error) {
		mu.Lock()
		banned = append(banned, ip)
		mu.Unlock()
		return "firewall", nil
	}
	e.Sync()
	time.Sleep(500 * time.Millisecond)
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	for i := 0; i < 10; i++ {
		fmt.Fprintf(f, "WARN login failed ip=80.94.95.115 n=%d\n", i)
	}
	fmt.Fprintln(f, "WARN login failed ip=185.224.128.9")
	f.Close()
	for i := 0; i < 40; i++ {
		mu.Lock()
		n := len(banned)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if fmt.Sprint(banned) != "[80.94.95.115]" {
		t.Fatalf("engellenenler: %v (durum %v)", banned, e.Status())
	}
	if h := e.Hits(); len(h) != 1 || !h[0].Blocked || h[0].Count != 10 {
		t.Fatalf("kayıt: %+v", h)
	}
	// kural kapatılınca izleme durur
	r.Enabled = false
	e.Sync()
	if len(e.Status()) != 0 {
		t.Fatalf("durum kaldı: %v", e.Status())
	}
}
