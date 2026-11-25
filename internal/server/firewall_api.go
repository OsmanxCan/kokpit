package server

import (
	"net/http"
	"strconv"

	"github.com/OsmanxCan/kokpit/internal/firewall"
)

func (s *Server) handleFirewall(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, firewall.Get())
}

func (s *Server) handleFirewallAdd(w http.ResponseWriter, r *http.Request) {
	var req firewall.NewRule
	if !readJSON(w, r, &req) {
		return
	}
	out, err := firewall.Add(req)
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "firewall kural ekle", req.Action+" "+req.Port+"/"+req.Proto+" "+req.From, err == nil, out)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	s.sec.Get(0)
	writeJSON(w, firewall.Get())
}

func (s *Server) handleFirewallDelete(w http.ResponseWriter, r *http.Request) {
	num, _ := strconv.Atoi(r.PathValue("num"))
	var req struct {
		To string `json:"to"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	out, err := firewall.Delete(num, req.To)
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "firewall kural sil", "#"+strconv.Itoa(num)+" "+req.To, err == nil, out)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, firewall.Get())
}

func (s *Server) handleFirewallState(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action   string `json:"action"` // enable | disable | confirm
		Rollback int    `json:"rollback"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	var out string
	var err error
	switch req.Action {
	case "enable":
		st := firewall.Get()
		if !st.SSHAllowed {
			writeErr(w, 400, "SSH portuna ("+strconv.Itoa(st.SSHPort)+") herkese izin veren bir kural yok. Firewall'u açarsan SSH ile bağlanamayabilirsin. Önce bir SSH kuralı ekle.")
			return
		}
		if req.Rollback < 0 || req.Rollback > 30 {
			req.Rollback = 5
		}
		out, err = firewall.Enable(req.Rollback)
	case "disable":
		out, err = firewall.Disable()
	case "confirm":
		firewall.CancelRollback()
	default:
		writeErr(w, 400, "bilinmeyen işlem")
		return
	}
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "firewall "+req.Action, "ufw", err == nil, out)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	s.sec.Get(0)
	writeJSON(w, firewall.Get())
}
