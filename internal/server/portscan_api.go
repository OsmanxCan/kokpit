package server

import (
	"net"
	"net/http"
	"time"

	"github.com/OsmanxCan/kokpit/internal/firewall"
)

func (s *Server) handleScans(w http.ResponseWriter, r *http.Request) {
	since, buckets := 24*time.Hour, 24
	if r.URL.Query().Get("range") == "7d" {
		since, buckets = 7*24*time.Hour, 7
	}
	rep := s.ps.Report(since, buckets)
	writeJSON(w, map[string]any{"report": rep, "bans": s.ps.Bans(), "settings": s.set.Get().PortScan, "ufwActive": firewall.Active()})
}

func (s *Server) handleScanBan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP     string `json:"ip"`
		Hours  int    `json:"hours"`
		Reason string `json:"reason"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	si := sess(r)
	var err error
	if net.ParseIP(req.IP) == nil {
		err = errBadIP
	} else if req.Hours < 0 || req.Hours > 24*365 {
		err = errBadHours
	} else {
		if req.Reason == "" {
			req.Reason = "elle engellendi"
		}
		err = s.ps.Ban(req.IP, req.Reason, req.Hours, si.Username)
	}
	s.st.Audit(si.Username, clientIP(r), "IP engelle", req.IP, err == nil, errStr(err))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleScanUnban(w http.ResponseWriter, r *http.Request) {
	ip := r.PathValue("ip")
	err := s.ps.Unban(ip)
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "IP engelini kaldır", ip, err == nil, errStr(err))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handlePorts(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("refresh") == "1" {
		s.pw.Refresh()
	}
	writeJSON(w, s.pw.Snapshot())
}

type apiErr string

func (e apiErr) Error() string { return string(e) }

const (
	errBadIP    apiErr = "geçersiz IP adresi"
	errBadHours apiErr = "süre 0 (kalıcı) ile 8760 saat arasında olmalı"
)
