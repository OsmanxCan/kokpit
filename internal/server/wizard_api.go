package server

import (
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
	user, ip := sess(r).Username, clientIP(r)
	id, err := s.Wizard.StartCreate(req, func(j *sitewizard.Job) {
		s.st.Audit(user, ip, "yeni site", j.Host, j.Error == "", strings.TrimSpace(j.Error+" "+stepsText(j.Steps)))
	})
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"job": id})
}

func (s *Server) handleWizardRemove(w http.ResponseWriter, r *http.Request) {
	host := r.PathValue("host")
	user, ip := sess(r).Username, clientIP(r)
	id, err := s.Wizard.StartRemove(host, func(j *sitewizard.Job) {
		s.st.Audit(user, ip, "siteyi kaldır", j.Host, j.Error == "", strings.TrimSpace(j.Error+" "+stepsText(j.Steps)))
	})
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"job": id})
}

func (s *Server) handleWizardJob(w http.ResponseWriter, r *http.Request) {
	j, ok := sitewizard.GetJob(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "iş bulunamadı (panel yeniden başlatılmış olabilir); Siteler listesini yenile")
		return
	}
	writeJSON(w, j)
}
