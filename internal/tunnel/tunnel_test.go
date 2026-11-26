package tunnel

import (
	"strings"
	"testing"
)

const sample = `tunnel: 6ff42ae2-765d-4adf-8112-31c55c1551ef
credentials-file: /root/.cloudflared/6ff42ae2-765d-4adf-8112-31c55c1551ef.json

# panel
ingress:
  - hostname: panel.example.com
    service: http://localhost:3100
  - hostname: "repo.example.com"
    service: http://localhost:80   # nginx
  - service: http_status:404
`

func TestParseAndEdit(t *testing.T) {
	c := Parse(sample)
	if c.TunnelID != "6ff42ae2-765d-4adf-8112-31c55c1551ef" || len(c.Rules) != 3 || c.Rules[1].Hostname != "repo.example.com" || c.Rules[1].Service != "http://localhost:80" {
		t.Fatalf("%+v", c)
	}
	out, err := c.WithRule("app.example.com", "http://localhost:80")
	if err != nil {
		t.Fatal(err)
	}
	want := "  - hostname: app.example.com\n    service: http://localhost:80\n  - service: http_status:404"
	if !strings.Contains(out, want) || !strings.Contains(out, "# panel") {
		t.Fatalf("ekleme:\n%s", out)
	}
	c2 := Parse(out)
	if len(c2.Rules) != 4 || c2.Rules[2].Hostname != "app.example.com" || c2.Rules[3].Hostname != "" {
		t.Fatalf("tekrar okuma: %+v", c2.Rules)
	}
	back, err := c2.WithoutRule("app.example.com")
	if err != nil || back != sample {
		t.Fatalf("kaldırma:\n%q\n%q\n%v", back, sample, err)
	}
	if _, err := c.WithRule("panel.example.com", "http://localhost:80"); err == nil {
		t.Error("var olan kural tekrar eklendi")
	}
	for _, bad := range [][2]string{{"x", "http://localhost:80"}, {"a.example.com", "http://evil.com"}, {"a.example.com", "http://localhost:80\n  - hostname: x"}} {
		if _, err := c.WithRule(bad[0], bad[1]); err == nil {
			t.Errorf("kabul edildi: %v", bad)
		}
	}
	// her şeyi yakalayan kural yoksa
	if _, err := Parse("tunnel: x\ningress:\n  - hostname: a.b.com\n    service: http://localhost:1\n").WithRule("c.b.com", "http://localhost:2"); err == nil {
		t.Error("catch-all olmadan eklendi")
	}
}
