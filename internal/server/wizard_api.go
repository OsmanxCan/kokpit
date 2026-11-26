package server

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/OsmanxCan/kokpit/internal/sitewizard"
	"github.com/OsmanxCan/kokpit/internal/tunnel"
)

func (s *Server) handleWizard(w http.ResponseWriter, r *http.Request) {
	path, ok := tunnel.Available()
	t := map[string]any{"available": ok, "path": path}
	if ok {
		if c, err := tunnel.Load(path); err == nil {
			t["tunnelId"], t["rules"] = c.TunnelID, c.Rules
		} else {
			t["error"] = err.Error()
		}
	}
	_, certErr := os.Stat("/root/.cloudflared/cert.pem")
	writeJSON(w, map[string]any{"tunnel": t, "cloudflare": s.CF.Enabled(), "cert": certErr == nil, "sites": s.Wizard.Sites()})
}

func (s *Server) handleWizardPreview(w http.ResponseWriter, r *http.Request) {
	var req sitewizard.Request
	if !readJSON(w, r, &req) {
		return
	}
	p, err := s.Wizard.Preview(req)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, p)
}

func stepsText(steps []sitewizard.Step) string {
	var l []string
	for _, st := range steps {
		l = append(l, st.Name+": "+st.Status)
	}
	return strings.Join(l, ", ")
}

func (s *Server) handleWizardCreate(w http.ResponseWriter, r *http.Request) {
	var req sitewizard.Request
	if !readJSON(w, r, &req) {
		return
	}
	steps, err := s.Wizard.Create(req)
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "yeni site", req.Host, err == nil, strings.TrimSpace(errStr(err)+" "+stepsText(steps)))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "steps": steps})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "steps": steps})
}

func (s *Server) handleWizardRemove(w http.ResponseWriter, r *http.Request) {
	host := r.PathValue("host")
	steps, err := s.Wizard.Remove(host)
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "siteyi kaldır", host, err == nil, strings.TrimSpace(errStr(err)+" "+stepsText(steps)))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "steps": steps})
}
