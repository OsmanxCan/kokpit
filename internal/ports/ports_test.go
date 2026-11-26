package ports

import (
	"testing"

	"github.com/OsmanxCan/kokpit/internal/firewall"
)

const ssOut = `udp   UNCONN 0      0          127.0.0.53%lo:53        0.0.0.0:*    users:(("systemd-resolve",pid=600,fd=13))
udp   UNCONN 0      0                0.0.0.0:60218     0.0.0.0:*    users:(("docker-proxy",pid=1200,fd=4))
udp   UNCONN 0      0                   [::]:60218        [::]:*    users:(("docker-proxy",pid=1201,fd=4))
tcp   LISTEN 0      4096             0.0.0.0:22        0.0.0.0:*    users:(("sshd",pid=900,fd=3))
tcp   LISTEN 0      4096                [::]:22           [::]:*    users:(("sshd",pid=900,fd=4))
tcp   LISTEN 0      511              0.0.0.0:80        0.0.0.0:*    users:(("nginx",pid=1000,fd=6),("nginx",pid=1001,fd=6))
tcp   LISTEN 0      511            127.0.0.1:3000      0.0.0.0:*    users:(("next-server",pid=1300,fd=20))
tcp   LISTEN 0      244              0.0.0.0:5432      0.0.0.0:*    users:(("postgres",pid=1400,fd=7))
tcp   LISTEN 0      4096           127.0.0.1:3100      0.0.0.0:*    users:(("kokpit",pid=1500,fd=8))
tcp   ESTAB  0      0          203.0.113.10:22   198.51.100.23:5555 users:(("sshd",pid=2000,fd=4))
tcp   LISTEN 0      511              0.0.0.0:8443      0.0.0.0:*    users:(("node",pid=1600,fd=9))
`

func TestClassify(t *testing.T) {
	items := ParseSS(ssOut)
	if len(items) != 8 {
		t.Fatalf("%d port: %+v", len(items), items)
	}
	fw := firewall.Status{Installed: true, Active: true, Incoming: "deny", Rules: []firewall.Rule{
		{To: "22/tcp", Action: "ALLOW IN", From: "Anywhere"},
		{To: "OpenSSH (v6)", Action: "ALLOW IN", From: "Anywhere (v6)"},
		{To: "80,443/tcp", Action: "ALLOW IN", From: "Anywhere"},
		{To: "8000:9000/tcp", Action: "ALLOW IN", From: "198.51.100.23"},
		{To: "Anywhere", Action: "DENY IN", From: "45.148.10.5"},
	}}
	got := map[string]Item{}
	for _, it := range Classify(items, fw) {
		got[it.Key()] = it
	}
	want := map[string]string{"udp/53": "local", "udp/60218": "open", "tcp/22": "open", "tcp/80": "open",
		"tcp/3000": "local", "tcp/5432": "protected", "tcp/3100": "local", "tcp/8443": "limited"}
	for k, w := range want {
		if got[k].Exposure != w {
			t.Errorf("%s: %q (%s), beklenen %q", k, got[k].Exposure, got[k].Reason, w)
		}
	}
	if !got["udp/60218"].Docker || got["tcp/22"].Program != "sshd" || len(got["tcp/22"].Addrs) != 2 {
		t.Errorf("ayrıntılar: %+v %+v", got["udp/60218"], got["tcp/22"])
	}
	// firewall kapalıysa postgres açık ve riskli
	fw.Active = false
	for _, it := range Classify(ParseSS(ssOut), fw) {
		if it.Port == 5432 && (it.Exposure != "open" || it.Risk != "PostgreSQL") {
			t.Errorf("postgres: %+v", it)
		}
	}
}

func TestNewPortAlert(t *testing.T) {
	w := NewWatcher()
	fw := firewall.Status{Installed: true, Active: true, Incoming: "deny", Rules: []firewall.Rule{{To: "22/tcp", Action: "ALLOW IN", From: "Anywhere"}}}
	w.Firewall = func() firewall.Status { return fw }
	var alerts []string
	w.OnNew = func(it Item) { alerts = append(alerts, it.Key()) }
	// Refresh ss çalıştırdığı için iç mantığı doğrudan sına
	w.apply(Classify(ParseSS(ssOut), fw))
	if len(alerts) != 0 {
		t.Fatalf("ilk okumada bildirim: %v", alerts)
	}
	fw.Rules = append(fw.Rules, firewall.Rule{To: "5432", Action: "ALLOW IN", From: "Anywhere"})
	w.apply(Classify(ParseSS(ssOut), fw))
	if len(alerts) != 1 || alerts[0] != "tcp/5432" {
		t.Fatalf("yeni açılan port: %v", alerts)
	}
	w.apply(Classify(ParseSS(ssOut), fw))
	if len(alerts) != 1 {
		t.Fatalf("tekrar bildirildi: %v", alerts)
	}
}
