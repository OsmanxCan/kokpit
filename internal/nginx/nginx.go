// Package nginx sites-enabled içindeki siteleri okur, SSL bitiş tarihlerini bulur.
package nginx

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/OsmanxCan/kokpit/internal/execx"
)

const SitesDir = "/etc/nginx/sites-enabled"

type Server struct {
	File       string   `json:"file"`
	Names      []string `json:"names"`
	Listen     []string `json:"listen"`
	ProxyPass  []string `json:"proxyPass"`
	Root       string   `json:"root"`
	SSL        bool     `json:"ssl"`
	CertFile   string   `json:"certFile"`
	CertExpiry int64    `json:"certExpiry"` // unix
	CertIssuer string   `json:"certIssuer"`
	Redirect   bool     `json:"redirect"`
}

// Sites tüm server bloklarını döner.
func Sites() ([]Server, error) {
	entries, err := os.ReadDir(SitesDir)
	if err != nil {
		return []Server{}, nil
	}
	var out []Server
	for _, e := range entries {
		p := filepath.Join(SitesDir, e.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, s := range parse(string(b)) {
			s.File = e.Name()
			if s.CertFile != "" {
				s.CertExpiry, s.CertIssuer = certInfo(s.CertFile)
			}
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out, nil
}

func stripComments(src string) string {
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// parse basit bir nginx ayrıştırıcısı: server { } bloklarındaki direktifleri toplar.
func parse(src string) []Server {
	src = stripComments(src)
	var servers []Server
	var stack []string
	var cur *Server
	var tok strings.Builder
	var stmt []string
	flushTok := func() {
		if tok.Len() > 0 {
			stmt = append(stmt, tok.String())
			tok.Reset()
		}
	}
	for _, r := range src {
		switch r {
		case '{':
			flushTok()
			name := ""
			if len(stmt) > 0 {
				name = stmt[0]
			}
			stack = append(stack, name)
			if name == "server" && len(stack) == 1 {
				cur = &Server{}
			}
			stmt = nil
		case '}':
			flushTok()
			stmt = nil
			if len(stack) > 0 {
				if stack[len(stack)-1] == "server" && len(stack) == 1 && cur != nil {
					servers = append(servers, *cur)
					cur = nil
				}
				stack = stack[:len(stack)-1]
			}
		case ';':
			flushTok()
			if cur != nil && len(stmt) > 0 {
				apply(cur, stmt)
			}
			stmt = nil
		case ' ', '\t', '\n', '\r':
			flushTok()
		default:
			tok.WriteRune(r)
		}
	}
	return servers
}

func apply(s *Server, st []string) {
	args := st[1:]
	switch st[0] {
	case "server_name":
		for _, a := range args {
			if a != "_" && !contains(s.Names, a) {
				s.Names = append(s.Names, a)
			}
		}
		if len(args) > 0 && args[0] == "_" && !contains(s.Names, "_ (varsayılan)") {
			s.Names = append(s.Names, "_ (varsayılan)")
		}
	case "listen":
		l := strings.Join(args, " ")
		s.Listen = append(s.Listen, l)
		if strings.Contains(l, "ssl") || strings.Contains(l, "443") {
			s.SSL = true
		}
	case "proxy_pass":
		if len(args) > 0 && !contains(s.ProxyPass, args[0]) {
			s.ProxyPass = append(s.ProxyPass, args[0])
		}
	case "root":
		if len(args) > 0 {
			s.Root = args[0]
		}
	case "ssl_certificate":
		if len(args) > 0 {
			s.CertFile = args[0]
			s.SSL = true
		}
	case "return":
		if len(args) > 0 && (args[0] == "301" || args[0] == "302") {
			s.Redirect = true
		}
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func certInfo(path string) (int64, string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, ""
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return 0, ""
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return 0, ""
	}
	return c.NotAfter.Unix(), c.Issuer.CommonName
}

// RemoteCert bir domainin internete sunduğu sertifikanın bitiş tarihini döner
// (Cloudflare arkasındaysa Cloudflare'in sertifikasıdır).
func RemoteCert(host string) (int64, string, error) {
	d := &net.Dialer{Timeout: 8 * time.Second}
	conn, err := tls.DialWithDialer(d, "tcp", host+":443", &tls.Config{ServerName: host})
	if err != nil {
		return 0, "", err
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return 0, "", nil
	}
	return certs[0].NotAfter.Unix(), certs[0].Issuer.CommonName, nil
}

func Test() (string, error) { return execx.Run("nginx", "-t") }

func Reload() (string, error) {
	if out, err := Test(); err != nil {
		return out, err
	}
	return execx.Run("systemctl", "reload", "nginx")
}
