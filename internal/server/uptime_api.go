package server

import (
	"net/http"
	"sort"
	"strings"

	"github.com/OsmanxCan/kokpit/internal/nginx"
	"github.com/OsmanxCan/kokpit/internal/uptime"
)

func (s *Server) handleUptimeList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.up.List())
}

func (s *Server) handleUptimeDetail(w http.ResponseWriter, r *http.Request) {
	d, err := s.up.Detail(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	writeJSON(w, d)
}

func (s *Server) handleUptimeAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		uptime.Monitor
		Enabled *bool `json:"enabled"` // gönderilmezse açık başlar
	}
	if !readJSON(w, r, &req) {
		return
	}
	m := req.Monitor
	m.Enabled = req.Enabled == nil || *req.Enabled
	m, err := s.up.Add(m)
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "uptime ekle", m.URL, err == nil, errStr(err))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	go s.up.CheckNow(m.ID)
	writeJSON(w, m)
}

func (s *Server) handleUptimeUpdate(w http.ResponseWriter, r *http.Request) {
	var m uptime.Monitor
	if !readJSON(w, r, &m) {
		return
	}
	m, err := s.up.Update(r.PathValue("id"), m)
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "uptime düzenle", m.URL, err == nil, errStr(err))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, m)
}

func (s *Server) handleUptimeDelete(w http.ResponseWriter, r *http.Request) {
	err := s.up.Delete(r.PathValue("id"))
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "uptime sil", r.PathValue("id"), err == nil, errStr(err))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleUptimeCheck(w http.ResponseWriter, r *http.Request) {
	c, err := s.up.CheckNow(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	writeJSON(w, c)
}

// handleUptimeSuggest Nginx'teki ve henüz izlenmeyen alan adlarını önerir.
func (s *Server) handleUptimeSuggest(w http.ResponseWriter, r *http.Request) {
	sites, _ := nginx.Sites()
	have := s.up.URLs()
	seen := map[string]bool{}
	out := []map[string]string{}
	for _, site := range sites {
		if site.Redirect && len(site.ProxyPass) == 0 && site.Root == "" {
			continue
		}
		for _, n := range site.Names {
			if !strings.Contains(n, ".") || strings.HasPrefix(n, "*") || strings.Contains(n, " ") {
				continue
			}
			u := "https://" + n
			if !site.SSL {
				u = "http://" + n
			}
			if have[u] || seen[u] {
				continue
			}
			seen[u] = true
			out = append(out, map[string]string{"name": n, "url": u})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"] < out[j]["name"] })
	writeJSON(w, out)
}
