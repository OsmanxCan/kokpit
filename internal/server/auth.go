package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/OsmanxCan/kokpit/internal/crypt"
	"github.com/OsmanxCan/kokpit/internal/store"
)

const (
	cookieName  = "kokpit_session"
	sessionIdle = 2 * time.Hour  // 2 saat işlem yapılmazsa oturum düşer
	sessionMax  = 12 * time.Hour // en fazla 12 saat
	maxFails    = 5              // 15 dakikada 5 hatalı deneme → kilit
	failWindow  = 15 * 60
)

type ctxKey string

type sessionInfo struct {
	Username string
	TokenH   string
}

// Kullanıcı yokken de hash hesaplayıp zamanlama farkını gizlemek için sahte hash.
var dummyHash, _ = crypt.HashPassword("kokpit-dummy")

// Aynı TOTP kodunun (zaman adımının) tekrar kullanılmasını engelle.
var usedSteps = struct {
	sync.Mutex
	m map[string]int64
}{m: map[string]int64{}}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

// clientIP: panel sadece 127.0.0.1'de dinlediği için istekler cloudflared'den gelir;
// gerçek IP Cloudflare'in CF-Connecting-IP başlığındadır.
func clientIP(r *http.Request) string {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cf != "" && net.ParseIP(cf) != nil {
			return cf
		}
	}
	return host
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	n := s.st.FailCount(ip, store.Now()-failWindow)
	if n >= maxFails {
		writeErr(w, 429, "Çok fazla hatalı deneme. 15 dakika sonra tekrar dene.")
		return
	}
	var req struct{ Username, Password, Code string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeErr(w, 400, "Geçersiz istek")
		return
	}
	username := strings.TrimSpace(req.Username)
	u, found := s.st.User(username)
	ok := false
	if !found {
		crypt.CheckPassword(dummyHash, req.Password)
	} else if crypt.CheckPassword(u.PassHash, req.Password) {
		if step, err := crypt.ValidateTOTP(u.TOTPSecret, req.Code); err == nil {
			usedSteps.Lock()
			if usedSteps.m[username] < step {
				usedSteps.m[username] = step
				ok = true
			}
			usedSteps.Unlock()
		}
	}
	if !ok {
		s.st.AddFail(ip)
		s.st.Audit(username, ip, "giris", "panel", false, "hatalı şifre veya 2FA")
		if n+1 >= 3 {
			s.alerts.Fire("panel_login_fail", ip, map[string]string{"ip": ip, "sayi": itoa(n + 1), "kullanici": username})
		}
		time.Sleep(700 * time.Millisecond)
		writeErr(w, 401, "Kullanıcı adı, şifre veya 2FA kodu hatalı")
		return
	}
	s.st.ClearFails(ip)
	s.issueSession(w, r, username)
	s.st.Audit(username, ip, "giris", "panel", true, r.UserAgent())
	s.alerts.Fire("panel_login", ip+username, map[string]string{"kullanici": username, "ip": ip})
	writeJSON(w, map[string]any{"ok": true, "username": username})
}

func (s *Server) session(r *http.Request) (*sessionInfo, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil || len(c.Value) != 64 {
		return nil, false
	}
	th := hashToken(c.Value)
	x, ok := s.st.Session(th)
	if !ok {
		return nil, false
	}
	now := store.Now()
	if now > x.ExpiresAt || now-x.LastSeen > int64(sessionIdle.Seconds()) || (s.set.Get().SessionIPBind && x.IP != clientIP(r)) {
		s.st.DeleteSession(th)
		return nil, false
	}
	if u, still := s.st.User(x.Username); !still || x.CreatedAt < u.UpdatedAt {
		s.st.DeleteSession(th)
		return nil, false
	}
	if now-x.LastSeen > 30 {
		s.st.TouchSession(th, now)
	}
	return &sessionInfo{x.Username, th}, true
}

// requireAuth oturum kontrolü + CSRF koruması (değiştiren isteklerde özel başlık zorunlu).
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		si, ok := s.session(r)
		if !ok {
			writeErr(w, 401, "Oturum yok veya süresi doldu")
			return
		}
		if r.Method != http.MethodGet && r.Header.Get("X-Panel") != "1" {
			writeErr(w, 403, "CSRF koruması: geçersiz istek")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey("s"), si)))
	}
}

func sess(r *http.Request) *sessionInfo {
	si, _ := r.Context().Value(ctxKey("s")).(*sessionInfo)
	if si == nil {
		return &sessionInfo{Username: "?"}
	}
	return si
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	si := sess(r)
	s.st.DeleteSession(si.TokenH)
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: !s.cfg.Dev, SameSite: http.SameSiteStrictMode})
	s.st.Audit(si.Username, clientIP(r), "cikis", "panel", true, "")
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	si := sess(r)
	us := s.upd.Status()
	writeJSON(w, map[string]any{"username": si.Username, "ip": clientIP(r), "telegram": s.alerts.TelegramReady(),
		"hostname": s.sys.Latest().Hostname, "version": us.Current, "repo": us.Repo, "updateAvailable": us.Available,
		"latestVersion": func() string {
			if us.Latest != nil {
				return us.Latest.Version
			}
			return ""
		}()})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	si := sess(r)
	type row struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		IP       string `json:"ip"`
		UA       string `json:"ua"`
		Created  int64  `json:"created"`
		LastSeen int64  `json:"lastSeen"`
		Current  bool   `json:"current"`
	}
	out := []row{}
	for _, x := range s.st.Sessions() {
		out = append(out, row{x.TokenHash[:16], x.Username, x.IP, x.UA, x.CreatedAt, x.LastSeen, x.TokenHash == si.TokenH})
	}
	writeJSON(w, out)
}

// Diğer tüm oturumları kapat
func (s *Server) handleKillSessions(w http.ResponseWriter, r *http.Request) {
	si := sess(r)
	n := s.st.DeleteSessionsExcept(si.TokenH)
	s.st.Audit(si.Username, clientIP(r), "oturumlari-kapat", "panel", true, itoa(n)+" oturum")
	writeJSON(w, map[string]any{"ok": true, "closed": n})
}

// issueSession yeni bir oturum açar ve çerezini yazar.
func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, username string) {
	raw := make([]byte, 32)
	rand.Read(raw)
	token := hex.EncodeToString(raw)
	now := store.Now()
	ua := r.UserAgent()
	if len(ua) > 300 {
		ua = ua[:300]
	}
	s.st.AddSession(store.Session{TokenHash: hashToken(token), Username: username, IP: clientIP(r), UA: ua,
		CreatedAt: now, LastSeen: now, ExpiresAt: now + int64(sessionMax.Seconds())})
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: !s.cfg.Dev,
		SameSite: http.SameSiteStrictMode, MaxAge: int(sessionMax.Seconds())})
}
