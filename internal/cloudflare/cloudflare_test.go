package cloudflare

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeCF struct {
	mu    sync.Mutex
	rules map[string]string // id → ip
	level string
	seq   int
}

func (f *fakeCF) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ok := func(v any) { json.NewEncoder(w).Encode(map[string]any{"success": true, "result": v}) }
	if r.Header.Get("Authorization") != "Bearer iyi-token" {
		w.WriteHeader(401)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []map[string]any{{"code": 1000, "message": "Invalid API Token"}}})
		return
	}
	p := r.URL.Path
	switch {
	case p == "/user/tokens/verify":
		ok(map[string]string{"status": "active"})
	case p == "/zones":
		ok([]map[string]any{{"id": "z1", "name": "example.com", "status": "active", "plan": map[string]string{"name": "Free Website"}}})
	case strings.HasSuffix(p, "/dns_records") && r.Method == "GET":
		if r.URL.Query().Get("name") == "eski.example.com" {
			ok([]map[string]any{{"id": "d0", "type": "A", "name": "eski.example.com", "content": "1.2.3.4"}})
			return
		}
		ok([]map[string]any{})
	case strings.HasSuffix(p, "/dns_records") && r.Method == "POST":
		ok(map[string]any{"id": "d1", "type": "CNAME"})
	case strings.Contains(p, "/dns_records/") && r.Method == "DELETE":
		ok(map[string]any{"id": "d1"})
	case strings.HasSuffix(p, "/settings/security_level"):
		if r.Method == "PATCH" {
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			f.level = b["value"]
		}
		ok(map[string]string{"value": f.level})
	case strings.HasSuffix(p, "/firewall/access_rules/rules") && r.Method == "POST":
		var b struct {
			Configuration map[string]string `json:"configuration"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &b)
		f.seq++
		id := "r" + string(rune('0'+f.seq))
		f.rules[id] = b.Configuration["value"]
		ok(map[string]string{"id": id})
	case strings.Contains(p, "/firewall/access_rules/rules/") && r.Method == "DELETE":
		id := p[strings.LastIndex(p, "/")+1:]
		delete(f.rules, id)
		ok(map[string]string{"id": id})
	default:
		w.WriteHeader(404)
	}
}

func TestClient(t *testing.T) {
	f := &fakeCF{rules: map[string]string{}, level: "medium"}
	srv := httptest.NewServer(f)
	defer srv.Close()
	token := "kotu"
	c := Open(t.TempDir(), func() string { return token })
	c.API = srv.URL
	if _, err := c.Verify(t.Context(), "kotu"); err == nil || !strings.Contains(err.Error(), "geçersiz") {
		t.Fatalf("kötü token: %v", err)
	}
	token = "iyi-token"
	zs, err := c.Verify(t.Context(), token)
	if err != nil || len(zs) != 1 || zs[0].Name != "example.com" {
		t.Fatalf("doğrulama: %v %+v", err, zs)
	}
	if err := c.BanIP("45.148.10.5", "web saldırısı", 24, "admin"); err != nil {
		t.Fatal(err)
	}
	if len(f.rules) != 1 || !c.Banned("45.148.10.5") {
		t.Fatalf("kural: %v", f.rules)
	}
	if err := c.UnbanIP("45.148.10.5"); err != nil || len(f.rules) != 0 || c.Banned("45.148.10.5") {
		t.Fatalf("kaldırma: %v %v", err, f.rules)
	}
	// süreli engel
	c.BanIP("2001:db8::5", "x", 1, "admin")
	c.mu.Lock()
	c.st.Bans[0].Until = time.Now().Add(-time.Minute).Unix()
	c.mu.Unlock()
	c.Expire()
	if len(f.rules) != 0 || len(c.Bans()) != 0 {
		t.Fatal("süresi dolan engel kalkmadı")
	}
	// under attack aç / kapat → önceki seviyeye dön
	f.level = "high"
	if err := c.SetUnderAttack("z1", true); err != nil || f.level != "under_attack" {
		t.Fatalf("under attack: %v %s", err, f.level)
	}
	if err := c.SetUnderAttack("z1", false); err != nil || f.level != "high" {
		t.Fatalf("geri alma: %v %s", err, f.level)
	}
	z, rec, created, err := c.EnsureCNAME("app.example.com", "t.cfargotunnel.com", "Kokpit")
	if err != nil || z != "z1" || rec != "d1" || !created {
		t.Fatalf("dns: %v %s %s %v", err, z, rec, created)
	}
	if _, _, _, err := c.EnsureCNAME("eski.example.com", "t.cfargotunnel.com", "x"); err == nil {
		t.Fatal("var olan kaydın üzerine yazıldı")
	}
	if _, _, _, err := c.EnsureCNAME("app.baska.com", "t.cfargotunnel.com", "x"); err == nil {
		t.Fatal("erişilemeyen alan adı kabul edildi")
	}
	if err := c.DeleteRecord("z1", "d1"); err != nil {
		t.Fatal(err)
	}
	if c.SetUnderAttack("yok", true) == nil {
		t.Fatal("bilinmeyen alan adı kabul edildi")
	}
}
