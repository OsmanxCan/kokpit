package firewall

import (
	"strings"
	"testing"
)

func TestArgs(t *testing.T) {
	ok := []struct {
		r    NewRule
		want string
	}{
		{NewRule{Action: "allow", Port: "22", Proto: "tcp", From: "any"}, "allow proto tcp from any to any port 22"},
		{NewRule{Action: "allow", Port: "8000:8100", Proto: "udp", From: "1.2.3.0/24", Comment: "Test (VPN)"}, "allow proto udp from 1.2.3.0/24 to any port 8000:8100 comment Test (VPN)"},
		{NewRule{Action: "deny", From: "45.12.8.3"}, "deny from 45.12.8.3 to any"},
		{NewRule{Action: "limit", Port: "22"}, "limit from any to any port 22"},
	}
	for _, c := range ok {
		a, err := c.r.args()
		if err != nil || strings.Join(a, " ") != c.want {
			t.Errorf("%+v → %v %v", c.r, a, err)
		}
	}
	bad := []NewRule{
		{Action: "allow; rm -rf /", Port: "22"},
		{Action: "allow", Port: "22; reboot"},
		{Action: "allow", Port: "70000"},
		{Action: "allow", Port: "1:2"},
		{Action: "allow", From: "any"},
		{Action: "allow", Port: "22", From: "abc"},
		{Action: "allow", Port: "22", Comment: "x'; reboot"},
	}
	for _, r := range bad {
		if _, err := r.args(); err == nil {
			t.Errorf("kabul edilmemeliydi: %+v", r)
		}
	}
}

func TestParse(t *testing.T) {
	l := "[ 3] 22/tcp                     LIMIT IN    Anywhere                   # SSH"
	m := ruleRe.FindStringSubmatch(l)
	if m == nil || m[1] != "3" || m[2] != "22/tcp" || m[3] != "LIMIT" || !strings.Contains(m[5], "# SSH") {
		t.Fatalf("%q", m)
	}
	l = "[12] 80,443/tcp (v6)            ALLOW IN    2400:cb00::/32             # Cloudflare"
	if m := ruleRe.FindStringSubmatch(l); m == nil || m[2] != "80,443/tcp (v6)" {
		t.Fatalf("%q", m)
	}
}
