package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/OsmanxCan/kokpit/internal/deploy"
)

type deployItem struct {
	deploy.Project
	Busy string            `json:"busy"`
	Last *deploy.Run       `json:"last"`
	Git  map[string]string `json:"git"`
}

func (s *Server) handleDeployList(w http.ResponseWriter, r *http.Request) {
	out := []deployItem{}
	for _, p := range s.dep.Projects() {
		it := deployItem{Project: p, Busy: s.dep.Busy(p.ID), Git: s.dep.Info(p)}
		if runs := s.dep.Runs(p.ID, 1); len(runs) > 0 {
			it.Last = &runs[0]
		}
		out = append(out, it)
	}
	var pm2 []string
	if items, err := s.svc.PM2(); err == nil {
		for _, it := range items {
			pm2 = append(pm2, it.Name)
		}
	}
	writeJSON(w, map[string]any{"projects": out, "pm2": pm2, "runs": s.dep.Runs("", 15)})
}

func (s *Server) handleDeploySave(w http.ResponseWriter, r *http.Request) {
	var p deploy.Project
	if !readJSON(w, r, &p) {
		return
	}
	p.ID = r.PathValue("id") // POST'ta boş → yeni proje
	p, err := s.dep.Save(p)
	si := sess(r)
	act := "deploy projesi ekle"
	if r.PathValue("id") != "" {
		act = "deploy projesi düzenle"
	}
	var cmds []string
	for _, st := range p.Steps {
		cmds = append(cmds, st.Cmd)
	}
	s.st.Audit(si.Username, clientIP(r), act, p.Name+" ("+p.Dir+"): "+strings.Join(cmds, " && "), err == nil, errStr(err))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, p)
}

func (s *Server) handleDeployDelete(w http.ResponseWriter, r *http.Request) {
	p, _ := s.dep.Project(r.PathValue("id"))
	err := s.dep.Delete(r.PathValue("id"))
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "deploy projesi sil", p.Name, err == nil, errStr(err))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleDeployRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target string `json:"target"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	si := sess(r)
	p, _ := s.dep.Project(r.PathValue("id"))
	run, err := s.dep.Start(r.PathValue("id"), deploy.Options{Target: req.Target, Trigger: "panel", User: si.Username})
	act := "deploy"
	if req.Target != "" {
		act = "deploy geri al (" + shortHash(req.Target) + ")"
	}
	s.st.Audit(si.Username, clientIP(r), act, p.Name, err == nil, errStr(err))
	if err != nil {
		writeErr(w, 409, err.Error())
		return
	}
	writeJSON(w, run)
}

func shortHash(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

func (s *Server) handleDeployRuns(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.dep.Runs(r.PathValue("id"), 30))
}

func (s *Server) handleDeployCommits(w http.ResponseWriter, r *http.Request) {
	c, err := s.dep.Commits(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, c)
}

func (s *Server) handleDeployCancel(w http.ResponseWriter, r *http.Request) {
	err := s.dep.Cancel(r.PathValue("rid"))
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "deploy durdur", r.PathValue("rid"), err == nil, errStr(err))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleDeployLog deploy çıktısını SSE ile canlı akıtır; bitince "done" olayı gönderir.
func (s *Server) handleDeployLog(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("rid")
	if _, ok := s.dep.Run(id); !ok {
		writeErr(w, 404, "deploy bulunamadı")
		return
	}
	f, ok := sseStart(w)
	if !ok {
		return
	}
	err := s.dep.Follow(r.Context(), id, func(b []byte) error {
		if b == nil {
			_, err := fmt.Fprint(w, ": ping\n\n")
			f.Flush()
			return err
		}
		j, _ := json.Marshal(string(b))
		_, err := fmt.Fprintf(w, "data: %s\n\n", j)
		f.Flush()
		return err
	})
	if err != nil && r.Context().Err() != nil {
		return
	}
	if err != nil {
		j, _ := json.Marshal(err.Error() + "\n")
		fmt.Fprintf(w, "data: %s\n\n", j)
	}
	run, _ := s.dep.Run(id)
	j, _ := json.Marshal(run)
	fmt.Fprintf(w, "event: done\ndata: %s\n\n", j)
	f.Flush()
}
