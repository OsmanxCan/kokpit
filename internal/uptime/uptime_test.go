package uptime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestDownUp(t *testing.T) {
	var mu sync.Mutex
	code := 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.WriteHeader(code)
		w.Write([]byte("merhaba kokpit"))
	}))
	defer srv.Close()
	e, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	e.OnEvent = func(ev Event) { events = append(events, ev.Kind) }
	m, err := e.Add(Monitor{URL: srv.URL, Enabled: true, Keyword: "kokpit"})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := e.CheckNow(m.ID)
	if !c.OK {
		t.Fatalf("ilk kontrol başarısız: %+v", c)
	}
	mu.Lock()
	code = 503
	mu.Unlock()
	e.CheckNow(m.ID) // 1. hata: henüz çöktü sayılmaz
	if len(events) != 0 {
		t.Fatalf("tek hatada olay üretildi: %v", events)
	}
	e.CheckNow(m.ID) // 2. hata: çöktü
	mu.Lock()
	code = 200
	mu.Unlock()
	e.CheckNow(m.ID) // düzeldi
	if len(events) != 2 || events[0] != "down" || events[1] != "up" {
		t.Fatalf("olaylar: %v", events)
	}
	d, _ := e.Detail(m.ID)
	if len(d.Incidents) != 1 || d.Incidents[0].End == 0 || d.Uptime24 != 50 {
		t.Fatalf("detay: %+v uptime=%v", d.Incidents, d.Uptime24)
	}
	// anahtar kelime
	m2, _ := e.Add(Monitor{URL: srv.URL, Enabled: true, Keyword: "yok-boyle-bir-sey"})
	if c, _ := e.CheckNow(m2.ID); c.OK || c.Error == "" {
		t.Fatalf("anahtar kelime kontrolü çalışmadı: %+v", c)
	}
}

func TestValidate(t *testing.T) {
	m := Monitor{URL: "app.example.com"}
	if err := Validate(&m); err != nil || m.URL != "https://app.example.com" || m.Interval != 60 || m.Name != "app.example.com" {
		t.Fatalf("%v %+v", err, m)
	}
	for _, bad := range []Monitor{{URL: "ftp://x"}, {URL: "https://x", Interval: 5}, {URL: "javascript:alert(1)"}} {
		if Validate(&bad) == nil {
			t.Errorf("kabul edilmemeliydi: %+v", bad)
		}
	}
}

func TestOIDC(t *testing.T) {
	var mu sync.Mutex
	issuer, keys := "", `[{"kty":"RSA","kid":"1"}]`
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			iss := issuer
			if iss == "" {
				iss = srv.URL
			}
			w.Write([]byte(`{"issuer":"` + iss + `","jwks_uri":"` + srv.URL + `/jwks","authorization_endpoint":"` + srv.URL + `/auth","token_endpoint":"` + srv.URL + `/token"}`))
		case "/jwks":
			w.Write([]byte(`{"keys":` + keys + `}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	e, _ := Open(t.TempDir())
	m, err := e.Add(Monitor{Kind: "oidc", URL: srv.URL + "/", Enabled: true})
	if err != nil || m.Issuer != srv.URL || m.URL != srv.URL+"/.well-known/openid-configuration" {
		t.Fatalf("%v %+v", err, m)
	}
	if c, _ := e.CheckNow(m.ID); !c.OK {
		t.Fatalf("sağlıklı sağlayıcı: %+v", c)
	}
	mu.Lock()
	issuer = "https://baska.example.com"
	mu.Unlock()
	if c, _ := e.CheckNow(m.ID); c.OK || !strings.Contains(c.Error, "issuer") {
		t.Fatalf("issuer: %+v", c)
	}
	mu.Lock()
	issuer, keys = "", "[]"
	mu.Unlock()
	if c, _ := e.CheckNow(m.ID); c.OK || !strings.Contains(c.Error, "anahtar") {
		t.Fatalf("jwks: %+v", c)
	}
}
