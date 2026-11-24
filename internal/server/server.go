// Package server HTTP API'si ve gömülü arayüzü sunar.
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OsmanxCan/kokpit/internal/alerts"
	"github.com/OsmanxCan/kokpit/internal/config"
	"github.com/OsmanxCan/kokpit/internal/nginx"
	"github.com/OsmanxCan/kokpit/internal/security"
	"github.com/OsmanxCan/kokpit/internal/services"
	"github.com/OsmanxCan/kokpit/internal/store"
	"github.com/OsmanxCan/kokpit/internal/system"
	"github.com/OsmanxCan/kokpit/internal/update"
)

type Server struct {
	cfg    *config.Config
	st     *store.Store
	sys    *system.Collector
	svc    *services.Manager
	sec    *security.Analyzer
	alerts *alerts.Engine
	upd    *update.Checker
	webFS  fs.FS
}

func New(cfg *config.Config, st *store.Store, sys *system.Collector, svc *services.Manager,
	sec *security.Analyzer, al *alerts.Engine, upd *update.Checker, web fs.FS) *Server {
	return &Server{cfg, st, sys, svc, sec, al, upd, web}
}

func itoa(n int) string { return strconv.Itoa(n) }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" || r.Header.Get("CF-Visitor") != "" {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	a := s.requireAuth

	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", a(s.handleLogout))
	mux.HandleFunc("GET /api/auth/me", a(s.handleMe))
	mux.HandleFunc("GET /api/auth/sessions", a(s.handleSessions))
	mux.HandleFunc("POST /api/auth/sessions/kill-others", a(s.handleKillSessions))

	mux.HandleFunc("GET /api/overview", a(s.handleOverview))
	mux.HandleFunc("GET /api/system", a(s.handleSystem))
	mux.HandleFunc("GET /api/system/history", a(s.handleHistory))
	mux.HandleFunc("GET /api/system/stream", a(s.handleStream))

	mux.HandleFunc("GET /api/services", a(s.handleServices))
	mux.HandleFunc("POST /api/services/{kind}/{name}/{action}", a(s.handleServiceAction))
	mux.HandleFunc("GET /api/services/{kind}/{name}/logs", a(s.handleLogs))

	mux.HandleFunc("GET /api/nginx", a(s.handleNginx))
	mux.HandleFunc("GET /api/nginx/remote-cert", a(s.handleRemoteCert))
	mux.HandleFunc("POST /api/nginx/{action}", a(s.handleNginxAction))

	mux.HandleFunc("GET /api/security", a(s.handleSecurity))
	mux.HandleFunc("POST /api/security/ban", a(s.handleBan))

	mux.HandleFunc("GET /api/alerts", a(s.handleAlerts))
	mux.HandleFunc("POST /api/alerts/test", a(s.handleAlertTest))
	mux.HandleFunc("GET /api/audit", a(s.handleAudit))

	mux.HandleFunc("GET /api/update", a(s.handleUpdateStatus))
	mux.HandleFunc("POST /api/update/check", a(s.handleUpdateCheck))
	mux.HandleFunc("POST /api/update/apply", a(s.handleUpdateApply))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeErr(w, 404, "Bulunamadı") })
	mux.Handle("/", s.spa())
	return securityHeaders(mux)
}

// spa gömülü React uygulamasını sunar; bilinmeyen yollar index.html'e düşer.
func (s *Server) spa() http.Handler {
	fileServer := http.FileServer(http.FS(s.webFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, err := s.webFS.Open(p); err == nil {
				f.Close()
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		b, err := fs.ReadFile(s.webFS, "index.html")
		if err != nil {
			http.Error(w, "Arayüz derlenmemiş (web/dist yok)", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(b)
	})
}

// ---------- genel bakış ----------

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	snap := s.sys.Latest()
	snap.Procs = nil
	type count struct {
		Total   int `json:"total"`
		Healthy int `json:"healthy"`
	}
	counts := map[string]*count{"pm2": {}, "docker": {}, "systemd": {}}
	var problems []services.Item
	var wg sync.WaitGroup
	var mu sync.Mutex
	for kind, fn := range map[string]func() ([]services.Item, error){
		"pm2": s.svc.PM2, "docker": s.svc.Docker, "systemd": func() ([]services.Item, error) { return s.svc.Systemd(false) },
	} {
		wg.Add(1)
		go func(kind string, fn func() ([]services.Item, error)) {
			defer wg.Done()
			items, err := fn()
			if err != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, it := range items {
				counts[kind].Total++
				if it.Healthy {
					counts[kind].Healthy++
				} else if kind != "systemd" || it.User == "izleniyor" || strings.HasPrefix(it.Status, "failed") {
					problems = append(problems, it)
				}
			}
		}(kind, fn)
	}
	wg.Wait()
	rep := s.sec.Get(5 * time.Minute)
	var failed []security.Check
	for _, c := range rep.Checks {
		if !c.OK {
			failed = append(failed, c)
		}
	}
	al := s.st.Alerts(8)
	sites, _ := nginx.Sites()
	writeJSON(w, map[string]any{
		"system": snap, "counts": counts, "problems": problems,
		"security": map[string]any{"score": rep.Score, "failed": failed, "sshFail24h": rep.SSHFail24h, "banned": len(rep.Banned), "ufw": rep.UFW},
		"alerts":   al, "sites": sites,
	})
}

// ---------- sistem ----------

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"latest": s.sys.Latest(), "ring": s.sys.Ring()})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	rng := map[string]int64{"1h": 3600, "6h": 6 * 3600, "24h": 86400, "7d": 7 * 86400}[r.URL.Query().Get("range")]
	if rng == 0 {
		rng = 86400
	}
	writeJSON(w, s.st.Metrics(store.Now()-rng, 300))
}

func sseStart(w http.ResponseWriter) (http.Flusher, bool) {
	f, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "akış desteklenmiyor")
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	f.Flush()
	return f, true
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	f, ok := sseStart(w)
	if !ok {
		return
	}
	ch, cancel := s.sys.Subscribe()
	defer cancel()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	send := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", b)
		f.Flush()
	}
	send(s.sys.Latest())
	for {
		select {
		case <-r.Context().Done():
			return
		case snap := <-ch:
			send(snap)
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			f.Flush()
		}
	}
}

// ---------- projeler / servisler ----------

func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "1"
	res := map[string]any{}
	errs := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for kind, fn := range map[string]func() ([]services.Item, error){
		"pm2": s.svc.PM2, "docker": s.svc.Docker, "systemd": func() ([]services.Item, error) { return s.svc.Systemd(all) },
	} {
		wg.Add(1)
		go func(kind string, fn func() ([]services.Item, error)) {
			defer wg.Done()
			items, err := fn()
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[kind] = err.Error()
				items = []services.Item{}
			}
			res[kind] = items
		}(kind, fn)
	}
	wg.Wait()
	res["errors"] = errs
	writeJSON(w, res)
}

func (s *Server) handleServiceAction(w http.ResponseWriter, r *http.Request) {
	kind, name, action := r.PathValue("kind"), r.PathValue("name"), r.PathValue("action")
	si := sess(r)
	out, err := s.svc.Action(kind, name, action)
	s.st.Audit(si.Username, clientIP(r), kind+" "+action, name, err == nil, out)
	if err != nil {
		writeErr(w, 500, strings.TrimSpace(err.Error()+"\n"+out))
		return
	}
	writeJSON(w, map[string]any{"ok": true, "output": out})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	kind, name := r.PathValue("kind"), r.PathValue("name")
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if lines <= 0 || lines > 2000 {
		lines = 200
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	cmd, err := s.svc.LogCommand(ctx, kind, name, lines)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	go func() { cmd.Wait(); pw.Close() }()
	go func() { <-ctx.Done(); pr.Close() }()
	f, ok := sseStart(w)
	if !ok {
		return
	}
	lineCh := make(chan string, 256)
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			select {
			case lineCh <- sc.Text():
			case <-ctx.Done():
				return
			}
		}
		close(lineCh)
	}()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	var batch []string
	flush := func() {
		if len(batch) == 0 {
			return
		}
		b, _ := json.Marshal(batch)
		fmt.Fprintf(w, "data: %s\n\n", b)
		f.Flush()
		batch = batch[:0]
	}
	for {
		select {
		case <-ctx.Done():
			return
		case l, ok := <-lineCh:
			if !ok {
				flush()
				fmt.Fprint(w, "event: end\ndata: {}\n\n")
				f.Flush()
				return
			}
			batch = append(batch, l)
			if len(batch) >= 200 {
				flush()
			}
		case <-tick.C:
			flush()
		}
	}
}

// ---------- nginx ----------

func (s *Server) handleNginx(w http.ResponseWriter, r *http.Request) {
	sites, err := nginx.Sites()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, sites)
}

func (s *Server) handleRemoteCert(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Query().Get("host")
	if host == "" || strings.ContainsAny(host, "/: ") {
		writeErr(w, 400, "geçersiz host")
		return
	}
	start := time.Now()
	exp, issuer, err := nginx.RemoteCert(host)
	res := map[string]any{"host": host, "expiry": exp, "issuer": issuer, "ms": time.Since(start).Milliseconds()}
	if err != nil {
		res["error"] = err.Error()
	}
	writeJSON(w, res)
}

func (s *Server) handleNginxAction(w http.ResponseWriter, r *http.Request) {
	si := sess(r)
	var out string
	var err error
	switch r.PathValue("action") {
	case "test":
		out, err = nginx.Test()
	case "reload":
		out, err = nginx.Reload()
		s.st.Audit(si.Username, clientIP(r), "nginx reload", "nginx", err == nil, out)
	default:
		writeErr(w, 404, "bilinmeyen işlem")
		return
	}
	writeJSON(w, map[string]any{"ok": err == nil, "output": out})
}

// ---------- güvenlik ----------

func (s *Server) handleSecurity(w http.ResponseWriter, r *http.Request) {
	age := 2 * time.Minute
	if r.URL.Query().Get("fresh") == "1" {
		age = 0
	}
	writeJSON(w, s.sec.Get(age))
}

func (s *Server) handleBan(w http.ResponseWriter, r *http.Request) {
	si := sess(r)
	var req struct {
		IP  string `json:"ip"`
		Ban bool   `json:"ban"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req) != nil {
		writeErr(w, 400, "geçersiz istek")
		return
	}
	if req.Ban && req.IP == clientIP(r) {
		writeErr(w, 400, "Kendi IP adresini banlayamazsın")
		return
	}
	out, err := security.Ban(req.IP, req.Ban)
	act := map[bool]string{true: "ip ban", false: "ip unban"}[req.Ban]
	s.st.Audit(si.Username, clientIP(r), act, req.IP, err == nil, out)
	if err != nil {
		writeErr(w, 500, strings.TrimSpace(err.Error()+" "+out))
		return
	}
	s.sec.Get(0)
	writeJSON(w, map[string]any{"ok": true})
}

// ---------- bildirim / kayıt ----------

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"alerts": s.st.Alerts(200), "telegram": s.alerts.TelegramReady()})
}

func (s *Server) handleAlertTest(w http.ResponseWriter, r *http.Request) {
	if err := s.alerts.Test(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.st.AuditList(300))
}

// ---------- güncelleme ----------

func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.upd.Status())
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	s.upd.Check()
	writeJSON(w, s.upd.Status())
}

func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	si := sess(r)
	from := s.upd.Current
	ver, err := s.upd.Apply()
	s.st.Audit(si.Username, clientIP(r), "guncelle", from+" → "+ver, err == nil, errStr(err))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.alerts.Fire("ok", "guncelleme", "upd"+ver, "Panel güncellendi: "+from+" → "+ver, time.Minute)
	s.st.Flush()
	writeJSON(w, map[string]any{"ok": true, "version": ver})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	// Yanıt gittikten sonra çık; systemd (Restart=always) yeni binary ile başlatır.
	go func() {
		time.Sleep(1500 * time.Millisecond)
		os.Exit(0)
	}()
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
