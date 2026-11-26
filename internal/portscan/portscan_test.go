package portscan

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/OsmanxCan/kokpit/internal/settings"
)

func line(ts float64, src string, dpt int, proto, flags string) string {
	return fmt.Sprintf("%.6f vps kernel: [UFW BLOCK] IN=eth0 OUT= MAC=aa:bb SRC=%s DST=203.0.113.10 LEN=44 TOS=0x00 PREC=0x00 TTL=242 ID=54321 PROTO=%s SPT=40000 DPT=%d WINDOW=1024 RES=0x00 %s URGP=0", ts, src, proto, dpt, flags)
}

func TestParse(t *testing.T) {
	cases := []struct {
		l    string
		ok   bool
		kind string
	}{
		{line(1764134400.5, "45.148.10.5", 8080, "TCP", "SYN"), true, "SYN"},
		{line(1764134400.5, "45.148.10.5", 8080, "TCP", "FIN PSH URG"), true, "XMAS"},
		{line(1764134400.5, "45.148.10.5", 8080, "TCP", "FIN"), true, "FIN"},
		{line(1764134400.5, "45.148.10.5", 8080, "TCP", ""), true, "NULL"},
		{line(1764134400.5, "45.148.10.5", 8080, "TCP", "ACK"), false, ""},
		{line(1764134400.5, "45.148.10.5", 8080, "TCP", "ACK RST"), false, ""},
		{"1764134400.500000 vps kernel: KOKPIT-TARAMA: IN=eth0 OUT= SRC=2001:db8::1 DST=2001:db8::2 PROTO=UDP SPT=1 DPT=161 LEN=8", true, "UDP"},
		{"1764134400.500000 vps kernel: [UFW BLOCK] IN= OUT=eth0 SRC=1.2.3.4 DST=5.6.7.8 PROTO=TCP SPT=1 DPT=80 SYN", false, ""}, // giden
		{"1764134400.500000 vps kernel: [UFW BLOCK] IN=eth0 OUT= SRC=1.2.3.4 DST=5.6.7.8 PROTO=ICMP TYPE=8", false, ""},
		{"çöp satır", false, ""},
	}
	for _, c := range cases {
		p, ok := ParseLine(c.l)
		if ok != c.ok || p.Kind != c.kind {
			t.Errorf("%q → %v %q", c.l, ok, p.Kind)
		}
	}
	p, _ := ParseLine(line(1764134400.25, "45.148.10.5", 22, "TCP", "SYN"))
	if p.TS.UnixMilli() != 1764134400250 || p.Port != 22 || p.Src != "45.148.10.5" {
		t.Errorf("%+v", p)
	}
}

func newEngine(t *testing.T, cfg settings.PortScan) (*Engine, *[]string) {
	if cfg.Threshold == 0 {
		cfg.Threshold, cfg.Window = 15, 60
	}
	e := Open(t.TempDir(), func() settings.PortScan { return cfg })
	var banned []string
	e.BanFunc = func(ip, c string) error { banned = append(banned, ip); return nil }
	e.UnbanFunc = func(ip string) error { banned = append(banned, "-"+ip); return nil }
	e.resolver = func(ctx context.Context, ip string) (string, bool) {
		switch ip {
		case "198.20.69.74":
			return "census9.shodan.io", true
		case "66.240.205.34":
			return "fake.shodan.io", false // ileri yönde doğrulanmayan sahte PTR
		}
		return "", false
	}
	return e, &banned
}

func feedScan(e *Engine, ip string, ports int, flags string, start time.Time) *Scan {
	var first *Scan
	for i := 0; i < ports; i++ {
		p, _ := ParseLine(line(float64(start.Add(time.Duration(i)*100*time.Millisecond).UnixMicro())/1e6, ip, 1000+i, "TCP", flags))
		if s := e.Feed(p); s != nil && first == nil {
			first = s
		}
	}
	return first
}

func TestDetectAndClassify(t *testing.T) {
	e, banned := newEngine(t, settings.PortScan{})
	now := time.Now()
	// birkaç port: tarama değil
	if s := feedScan(e, "203.0.113.50", 5, "SYN", now); s != nil {
		t.Fatal("5 port tarama sayıldı")
	}
	// hızlı SYN taraması
	s := feedScan(e, "45.148.10.5", 300, "SYN", now)
	if s == nil {
		t.Fatal("tarama yakalanmadı")
	}
	e.Classify(*s)
	// bilinen tarayıcı
	si := feedScan(e, "198.20.69.74", 50, "SYN", now)
	e.Classify(*si)
	// sahte PTR: hedefli sayılmalı
	sf := feedScan(e, "66.240.205.34", 50, "SYN", now)
	e.Classify(*sf)
	// gizli tarama: 3 port yeter
	sx := feedScan(e, "185.224.128.9", 4, "FIN PSH URG", now)
	if sx == nil {
		t.Fatal("XMAS taraması yakalanmadı")
	}
	e.Classify(*sx)

	r := e.Report(24*time.Hour, 24)
	kinds := map[string]string{}
	for _, x := range r.Scans {
		kinds[x.IP] = x.Kind
	}
	want := map[string]string{"45.148.10.5": "targeted", "198.20.69.74": "internet", "66.240.205.34": "targeted", "185.224.128.9": "targeted"}
	for ip, k := range want {
		if kinds[ip] != k {
			t.Errorf("%s: %q, beklenen %q (%v)", ip, kinds[ip], k, kinds)
		}
	}
	for _, x := range r.Scans {
		if x.IP == "45.148.10.5" && (x.Ports != 300 || x.Types[0] != "SYN" || len(x.Sample) != 40) {
			t.Errorf("tarama bilgisi: %+v", x)
		}
	}
	if r.Internet != 1 || r.Targeted != 3 {
		t.Errorf("sayılar: %+v", r)
	}
	if len(*banned) != 0 {
		t.Errorf("otomatik engelleme kapalıyken engellendi: %v", *banned)
	}
	// bitiş ve kayıt
	e.Tick(now.Add(10 * time.Minute))
	if r2 := e.Report(24*time.Hour, 24); len(r2.Scans) != 4 || r2.Scans[0].Open {
		t.Errorf("kapanış: %d", len(r2.Scans))
	}
	// diskten tekrar açılış
	e2 := Open(filepathDir(e), e.cfg)
	if len(e2.Report(24*time.Hour, 24).Scans) != 4 {
		t.Error("kayıtlar diske yazılmadı")
	}
}

func filepathDir(e *Engine) string { return e.path[:len(e.path)-len("/portscan.json")] }

func TestAutoBan(t *testing.T) {
	e, banned := newEngine(t, settings.PortScan{AutoBan: true, BanHours: 24, Whitelist: []string{"203.0.113.0/24"}})
	now := time.Now()
	e.Classify(*feedScan(e, "45.148.10.5", 100, "SYN", now))
	e.Classify(*feedScan(e, "198.20.69.74", 100, "SYN", now)) // internet geneli: engellenmez
	e.Classify(*feedScan(e, "203.0.113.77", 100, "SYN", now)) // beyaz listede
	e.Classify(*feedScan(e, "104.16.5.5", 100, "SYN", now))   // Cloudflare
	if fmt.Sprint(*banned) != "[45.148.10.5]" {
		t.Fatalf("engellenenler: %v", *banned)
	}
	b := e.Bans()
	if len(b) != 1 || b[0].Until < now.Add(23*time.Hour).Unix() || !b[0].Auto {
		t.Fatalf("ban: %+v", b)
	}
	// süre dolunca kalkar
	e.Tick(now.Add(25 * time.Hour))
	if len(e.Bans()) != 0 || (*banned)[len(*banned)-1] != "-45.148.10.5" {
		t.Fatalf("süre dolunca kalkmadı: %v %v", e.Bans(), *banned)
	}
	// elle engelleme ve koruma
	if err := e.Ban("10.0.0.5", "deneme", 1, "can"); err == nil {
		t.Error("özel ağ adresi engellendi")
	}
	e.Protected = func(ip string) bool { return ip == "198.51.100.23" }
	if err := e.Ban("198.51.100.23", "deneme", 1, "can"); err == nil {
		t.Error("panel kullanıcısının IP'si engellendi")
	}
	if err := e.Ban("45.148.10.6", "deneme", 0, "can"); err != nil {
		t.Fatal(err)
	}
	if b := e.Bans(); len(b) != 1 || b[0].Until != 0 {
		t.Fatalf("kalıcı ban: %+v", b)
	}
	if err := e.Unban("45.148.10.6"); err != nil || len(e.Bans()) != 0 {
		t.Fatal(err)
	}
}

func TestOff(t *testing.T) {
	e, _ := newEngine(t, settings.PortScan{Off: true})
	if feedScan(e, "45.148.10.5", 100, "SYN", time.Now()) != nil {
		t.Fatal("kapalıyken tarama tespit edildi")
	}
}
