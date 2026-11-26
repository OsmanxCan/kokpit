// Package firewall UFW kurallarını okur ve değiştirir.
// Tüm komutlar shell olmadan çalıştırılır ve girdiler sıkı biçimde doğrulanır.
package firewall

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/OsmanxCan/kokpit/internal/execx"
)

type Rule struct {
	Num     int    `json:"num"`
	To      string `json:"to"`
	Action  string `json:"action"` // ALLOW IN, DENY IN, LIMIT IN, ALLOW FWD...
	From    string `json:"from"`
	Comment string `json:"comment"`
	V6      bool   `json:"v6"`
	SSH     bool   `json:"ssh"` // SSH portuna izin veren kural
}

type Status struct {
	Installed  bool   `json:"installed"`
	Active     bool   `json:"active"`
	Incoming   string `json:"incoming"` // deny / allow / reject
	Outgoing   string `json:"outgoing"`
	Routed     string `json:"routed"`
	Rules      []Rule `json:"rules"`
	SSHPort    int    `json:"sshPort"`
	SSHAllowed bool   `json:"sshAllowed"`
	Rollback   int64  `json:"rollback"` // otomatik geri alma zamanı (unix), 0 = yok
	Raw        string `json:"raw"`
}

const rollbackUnit = "kokpit-ufw-geri"

var (
	ruleRe    = regexp.MustCompile(`^\[\s*(\d+)\]\s+(.+?)\s{2,}(ALLOW|DENY|REJECT|LIMIT)\s+(IN|OUT|FWD)?\s*(.*)$`)
	defaultRe = regexp.MustCompile(`Default:\s*(\w+)\s*\(incoming\),\s*(\w+)\s*\(outgoing\)(?:,\s*(\w+)\s*\(routed\))?`)
)

var rollbackAt int64

func SSHPort() int {
	out, _ := execx.Run("sshd", "-T")
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) == 2 && f[0] == "port" {
			if p, err := strconv.Atoi(f[1]); err == nil {
				return p
			}
		}
	}
	return 22
}

func Get() Status {
	s := Status{SSHPort: SSHPort(), Rules: []Rule{}}
	if !execx.Exists("ufw") {
		return s
	}
	s.Installed = true
	verbose, _ := execx.Run("ufw", "status", "verbose")
	s.Raw = verbose
	s.Active = strings.Contains(verbose, "Status: active")
	if m := defaultRe.FindStringSubmatch(verbose); m != nil {
		s.Incoming, s.Outgoing, s.Routed = m[1], m[2], m[3]
	}
	numbered, _ := execx.Run("ufw", "status", "numbered")
	sshStr := strconv.Itoa(s.SSHPort)
	for _, l := range strings.Split(numbered, "\n") {
		m := ruleRe.FindStringSubmatch(strings.TrimRight(l, " "))
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		r := Rule{Num: n, To: strings.TrimSpace(m[2]), Action: strings.TrimSpace(m[3] + " " + m[4])}
		from := strings.TrimSpace(m[5])
		if i := strings.Index(from, "#"); i >= 0 {
			r.Comment = strings.TrimSpace(from[i+1:])
			from = strings.TrimSpace(from[:i])
		}
		r.From = from
		r.V6 = strings.Contains(r.To, "(v6)") || strings.Contains(r.From, "(v6)")
		port := strings.Fields(strings.ReplaceAll(r.To, "(v6)", ""))
		if len(port) > 0 {
			p := strings.Split(port[0], "/")[0]
			if (p == sshStr || p == "OpenSSH") && (strings.HasPrefix(r.Action, "ALLOW") || strings.HasPrefix(r.Action, "LIMIT")) {
				r.SSH = true
				if strings.HasPrefix(r.From, "Anywhere") {
					s.SSHAllowed = true
				}
			}
		}
		s.Rules = append(s.Rules, r)
	}
	if rollbackAt > time.Now().Unix() {
		if out, _ := execx.Run("systemctl", "is-active", rollbackUnit+".timer"); strings.TrimSpace(out) == "active" {
			s.Rollback = rollbackAt
		}
	}
	return s
}

// NewRule yeni bir kural isteği.
type NewRule struct {
	Action  string `json:"action"` // allow | deny | limit
	Port    string `json:"port"`   // "22", "8000:8100" veya boş (tüm portlar, sadece kaynak IP verilirse)
	Proto   string `json:"proto"`  // tcp | udp | any
	From    string `json:"from"`   // "any", IP veya CIDR
	Comment string `json:"comment"`
}

var portRe = regexp.MustCompile(`^\d{1,5}(:\d{1,5})?$`)
var commentRe = regexp.MustCompile(`^[\p{L}\p{N} ._\-/()]{0,60}$`)

func (r NewRule) args() ([]string, error) {
	switch r.Action {
	case "allow", "deny", "limit":
	default:
		return nil, errors.New("işlem allow, deny veya limit olmalı")
	}
	proto := r.Proto
	if proto == "" {
		proto = "any"
	}
	if proto != "tcp" && proto != "udp" && proto != "any" {
		return nil, errors.New("protokol tcp, udp veya her ikisi olmalı")
	}
	from := strings.TrimSpace(r.From)
	if from == "" || strings.EqualFold(from, "any") || from == "herkes" {
		from = "any"
	} else if net.ParseIP(from) == nil {
		if _, _, err := net.ParseCIDR(from); err != nil {
			return nil, errors.New("kaynak geçerli bir IP adresi veya CIDR olmalı (örn. 1.2.3.4 veya 1.2.3.0/24)")
		}
	}
	port := strings.TrimSpace(r.Port)
	if port != "" {
		if !portRe.MatchString(port) {
			return nil, errors.New("port bir sayı (22) veya aralık (8000:8100) olmalı")
		}
		for _, p := range strings.Split(port, ":") {
			if n, _ := strconv.Atoi(p); n < 1 || n > 65535 {
				return nil, errors.New("port 1 ile 65535 arasında olmalı")
			}
		}
		if strings.Contains(port, ":") && proto == "any" {
			return nil, errors.New("port aralıklarında protokol (tcp veya udp) seçilmeli")
		}
	} else if from == "any" {
		return nil, errors.New("port boşsa bir kaynak IP girilmeli (o IP'nin tüm trafiği için)")
	}
	if !commentRe.MatchString(r.Comment) {
		return nil, errors.New("açıklama en fazla 60 karakter olabilir; harf, rakam, boşluk ve . _ - / ( ) kullanılabilir")
	}
	args := []string{r.Action}
	if proto != "any" {
		args = append(args, "proto", proto)
	}
	args = append(args, "from", from, "to", "any")
	if port != "" {
		args = append(args, "port", port)
	}
	if c := strings.TrimSpace(r.Comment); c != "" {
		args = append(args, "comment", c)
	}
	return args, nil
}

func Add(r NewRule) (string, error) {
	args, err := r.args()
	if err != nil {
		return "", err
	}
	out, err := execx.Run("ufw", args...)
	if err != nil {
		return out, fmt.Errorf("ufw: %s", strings.TrimSpace(out))
	}
	return out, nil
}

// Delete numaralı kuralı siler. expect, silinecek kuralın beklenen "to" değeridir;
// arada kurallar değiştiyse yanlış kural silinmesin diye kontrol edilir.
func Delete(num int, expectTo string) (string, error) {
	cur := Get()
	var found *Rule
	for i := range cur.Rules {
		if cur.Rules[i].Num == num {
			found = &cur.Rules[i]
		}
	}
	if found == nil {
		return "", errors.New("kural bulunamadı, listeyi yenile")
	}
	if found.To != expectTo {
		return "", errors.New("kurallar değişmiş, listeyi yenileyip tekrar dene")
	}
	out, err := execx.Run("ufw", "--force", "delete", strconv.Itoa(num))
	if err != nil {
		return out, fmt.Errorf("ufw: %s", strings.TrimSpace(out))
	}
	return out, nil
}

// Enable UFW'yi açar; rollbackMin > 0 ise o kadar dakika sonra otomatik kapatır
// (Confirm çağrılmazsa). Böylece yanlış kuralla sunucunun dışında kalınmaz.
func Enable(rollbackMin int) (string, error) {
	if rollbackMin > 0 && execx.Exists("systemd-run") {
		execx.Run("systemctl", "stop", rollbackUnit+".timer")
		ufwPath := "/usr/sbin/ufw"
		if out, err := execx.Run("systemd-run", "--quiet", "--unit="+rollbackUnit, "--on-active="+strconv.Itoa(rollbackMin*60), ufwPath, "disable"); err != nil {
			return out, fmt.Errorf("otomatik geri alma kurulamadı: %s", strings.TrimSpace(out))
		}
		rollbackAt = time.Now().Add(time.Duration(rollbackMin) * time.Minute).Unix()
	}
	out, err := execx.Run("ufw", "--force", "enable")
	if err != nil {
		CancelRollback()
		return out, fmt.Errorf("ufw: %s", strings.TrimSpace(out))
	}
	return out, nil
}

// CancelRollback otomatik geri almayı iptal eder ("her şey çalışıyor" onayı).
func CancelRollback() {
	execx.Run("systemctl", "stop", rollbackUnit+".timer")
	execx.Run("systemctl", "reset-failed", rollbackUnit+".service")
	rollbackAt = 0
}

func Disable() (string, error) {
	CancelRollback()
	out, err := execx.Run("ufw", "disable")
	if err != nil {
		return out, fmt.Errorf("ufw: %s", strings.TrimSpace(out))
	}
	return out, nil
}

// ---------- süreli engelleme (port taraması vb.) ----------

// Ban IP'den gelen her şeyi en üstteki kuralla engeller. Var olan izin kurallarından
// önce uygulanması için "prepend" kullanılır.
func Ban(ip, comment string) error {
	if net.ParseIP(ip) == nil {
		return errors.New("geçersiz IP")
	}
	if len(comment) > 60 {
		comment = comment[:60]
	}
	out, err := execx.Run("ufw", "prepend", "deny", "from", ip, "to", "any", "comment", comment)
	if err != nil {
		return fmt.Errorf("ufw: %s", strings.TrimSpace(out))
	}
	return nil
}

// Unban Ban ile eklenen kuralı kaldırır.
func Unban(ip string) error {
	if net.ParseIP(ip) == nil {
		return errors.New("geçersiz IP")
	}
	out, err := execx.Run("ufw", "--force", "delete", "deny", "from", ip, "to", "any")
	if err != nil && !strings.Contains(out, "Could not delete non-existent rule") {
		return fmt.Errorf("ufw: %s", strings.TrimSpace(out))
	}
	return nil
}

// Active UFW açık mı.
func Active() bool {
	out, _ := execx.Run("ufw", "status")
	return strings.Contains(out, "Status: active")
}
