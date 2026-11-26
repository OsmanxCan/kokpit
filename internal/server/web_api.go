package server

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/OsmanxCan/kokpit/internal/logrules"
	"github.com/OsmanxCan/kokpit/internal/settings"
	"github.com/OsmanxCan/kokpit/internal/weblog"
)

// ---------- web logları ----------

var realIPCache struct {
	sync.Mutex
	v  weblog.RealIP
	at time.Time
}

func realIPStatus(fresh bool) weblog.RealIP {
	realIPCache.Lock()
	defer realIPCache.Unlock()
	if fresh || time.Since(realIPCache.at) > time.Minute {
		realIPCache.v, realIPCache.at = weblog.CheckRealIP(), time.Now()
	}
	return realIPCache.v
}

func (s *Server) handleWebReport(w http.ResponseWriter, r *http.Request) {
	rng := r.URL.Query().Get("range")
	if rng != "7d" {
		rng = "24h"
	}
	fresh := r.URL.Query().Get("fresh") == "1"
	writeJSON(w, map[string]any{"report": s.Web.Report(rng, fresh), "realip": realIPStatus(fresh), "cloudflare": s.CF.Enabled()})
}

func (s *Server) handleRealIPApply(w http.ResponseWriter, r *http.Request) {
	out, err := weblog.ApplyRealIP()
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "nginx gerçek IP ayarı", weblog.RealIPFile, err == nil, strings.TrimSpace(errStr(err)+" "+out))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, realIPStatus(true))
}

func (s *Server) handleRealIPRemove(w http.ResponseWriter, r *http.Request) {
	err := weblog.RemoveRealIP()
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "nginx gerçek IP ayarını kaldır", weblog.RealIPFile, err == nil, errStr(err))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, realIPStatus(true))
}

func (s *Server) handleWebBan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP     string `json:"ip"`
		Hours  int    `json:"hours"`
		Target string `json:"target"` // auto | cloudflare | firewall
		Reason string `json:"reason"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	si := sess(r)
	var backend string
	var err error
	switch {
	case net.ParseIP(req.IP) == nil:
		err = errBadIP
	case req.Hours < 0 || req.Hours > 24*365:
		err = errBadHours
	case req.Target != "auto" && req.Target != "cloudflare" && req.Target != "firewall":
		err = apiErr("geçersiz hedef")
	default:
		if req.Reason == "" {
			req.Reason = "web saldırısı"
		}
		backend, err = s.BanAny(req.IP, req.Reason, req.Hours, req.Target, si.Username)
	}
	s.st.Audit(si.Username, clientIP(r), "IP engelle ("+req.Target+")", req.IP, err == nil, errStr(err))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "backend": backend})
}

// ---------- Cloudflare ----------

func (s *Server) handleCF(w http.ResponseWriter, r *http.Request) {
	cur := s.set.Get()
	resp := map[string]any{"enabled": cur.CFToken != "", "masked": settings.MaskToken(cur.CFToken), "bans": s.CF.Bans()}
	if cur.CFToken != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		zs, err := s.CF.Zones(ctx, r.URL.Query().Get("fresh") == "1")
		resp["zones"] = zs
		if err != nil {
			resp["error"] = err.Error()
		}
	}
	writeJSON(w, resp)
}

func (s *Server) handleCFToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	tok := strings.TrimSpace(req.Token)
	si := sess(r)
	if len(tok) < 20 || len(tok) > 200 || strings.ContainsAny(tok, " \t\n") {
		writeErr(w, 400, "Token geçersiz görünüyor")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	zs, err := s.CF.Verify(ctx, tok)
	if err == nil {
		err = s.set.Update(func(c *settings.Settings) { c.CFToken = tok })
	}
	s.st.Audit(si.Username, clientIP(r), "cloudflare token kaydet", settings.MaskToken(tok), err == nil, errStr(err))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	s.CF.Zones(ctx, true)
	writeJSON(w, map[string]any{"ok": true, "zones": zs})
}

func (s *Server) handleCFTokenDelete(w http.ResponseWriter, r *http.Request) {
	err := s.set.Update(func(c *settings.Settings) { c.CFToken = "" })
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "cloudflare token sil", "", err == nil, errStr(err))
	writeJSON(w, map[string]any{"ok": err == nil})
}

func (s *Server) handleCFAttack(w http.ResponseWriter, r *http.Request) {
	var req struct {
		On bool `json:"on"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	err := s.CF.SetUnderAttack(r.PathValue("id"), req.On)
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), map[bool]string{true: "under attack aç", false: "under attack kapat"}[req.On], r.PathValue("id"), err == nil, errStr(err))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleCFEvents(w http.ResponseWriter, r *http.Request) {
	ev, err := s.CF.Events(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, ev)
}

func (s *Server) handleCFUnban(w http.ResponseWriter, r *http.Request) {
	ip := r.PathValue("ip")
	err := s.CF.UnbanIP(ip)
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "cloudflare engelini kaldır", ip, err == nil, errStr(err))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// ---------- log kuralları ----------

func (s *Server) handleLogRules(w http.ResponseWriter, r *http.Request) {
	var pm2 []string
	if items, err := s.svc.PM2(); err == nil {
		for _, it := range items {
			pm2 = append(pm2, it.Name)
		}
	}
	writeJSON(w, map[string]any{"rules": s.set.Get().LogRules, "status": s.Rules.Status(), "hits": s.Rules.Hits(),
		"pm2": pm2, "cloudflare": s.CF.Enabled()})
}

func (s *Server) handleLogRuleSave(w http.ResponseWriter, r *http.Request) {
	var rule settings.LogRule
	if !readJSON(w, r, &rule) {
		return
	}
	id := r.PathValue("id")
	err := logrules.Validate(&rule)
	if err == nil {
		err = s.set.Update(func(c *settings.Settings) {
			if id == "" {
				if len(c.LogRules) >= 20 {
					err = apiErr("en fazla 20 kural eklenebilir")
					return
				}
				rule.ID = newRuleID()
				c.LogRules = append(c.LogRules, rule)
				return
			}
			for i := range c.LogRules {
				if c.LogRules[i].ID == id {
					rule.ID = id
					c.LogRules[i] = rule
					return
				}
			}
			err = apiErr("kural bulunamadı")
		})
	}
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), map[bool]string{true: "log kuralı ekle", false: "log kuralı düzenle"}[id == ""],
		rule.Name+" ("+rule.Source+": "+rule.Path+")", err == nil, errStr(err))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	s.Rules.Sync()
	writeJSON(w, rule)
}

func (s *Server) handleLogRuleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var name string
	err := s.set.Update(func(c *settings.Settings) {
		keep := c.LogRules[:0]
		for _, x := range c.LogRules {
			if x.ID == id {
				name = x.Name
				continue
			}
			keep = append(keep, x)
		}
		c.LogRules = keep
	})
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "log kuralı sil", name, err == nil, errStr(err))
	s.Rules.Sync()
	writeJSON(w, map[string]any{"ok": err == nil})
}

func (s *Server) handleLogRuleTest(w http.ResponseWriter, r *http.Request) {
	var rule settings.LogRule
	if !readJSON(w, r, &rule) {
		return
	}
	if rule.Name == "" {
		rule.Name = "deneme"
	}
	if err := logrules.Validate(&rule); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	res, err := logrules.Test(rule, func(n string) []string { f, _ := s.svc.PM2LogFiles(n); return f })
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, res)
}

func newRuleID() string {
	return strings.ReplaceAll(time.Now().Format("060102150405.000"), ".", "")
}
