package server

import (
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/OsmanxCan/kokpit/internal/geoip"
	"github.com/OsmanxCan/kokpit/internal/security"
)

type countryStat struct {
	CC    string `json:"cc"`
	Count int    `json:"count"`
	IPs   int    `json:"ips"`
}

type ipStat struct {
	IP     string `json:"ip"`
	CC     string `json:"cc"`
	Count  int    `json:"count"`
	Users  string `json:"users"`
	Last   int64  `json:"last"`
	Banned bool   `json:"banned"`
}

type attackReport struct {
	Range     string        `json:"range"`
	Generated int64         `json:"generated"`
	Total     int           `json:"total"`
	UniqueIPs int           `json:"uniqueIps"`
	Countries []countryStat `json:"countries"`
	TopIPs    []ipStat      `json:"topIps"`
	Hourly    []int         `json:"hourly"` // son 24 saat (veya 7 gün için günlük)
	Accepted  int           `json:"accepted"`
	Geo       geoip.Status  `json:"geo"`
}

var attackCache = struct {
	sync.Mutex
	m map[string]*attackReport
}{m: map[string]*attackReport{}}

func (s *Server) handleAttacks(w http.ResponseWriter, r *http.Request) {
	rng := r.URL.Query().Get("range")
	if rng != "7d" {
		rng = "24h"
	}
	attackCache.Lock()
	c := attackCache.m[rng]
	attackCache.Unlock()
	fresh := r.URL.Query().Get("fresh") == "1"
	// ülke veritabanı yeni hazır olduysa önbelleği yenile
	if c != nil && !fresh && time.Since(time.Unix(c.Generated, 0)) < 10*time.Minute && c.Geo.Ready == s.geo.Status().Ready {
		c.Geo = s.geo.Status()
		writeJSON(w, c)
		return
	}
	rep := s.buildAttacks(rng)
	attackCache.Lock()
	attackCache.m[rng] = rep
	attackCache.Unlock()
	writeJSON(w, rep)
}

func (s *Server) buildAttacks(rng string) *attackReport {
	since, buckets, bucketSec := "-24h", 24, int64(3600)
	if rng == "7d" {
		since, buckets, bucketSec = "-7d", 7, 86400
	}
	now := time.Now()
	rep := &attackReport{Range: rng, Generated: now.Unix(), Hourly: make([]int, buckets), Geo: s.geo.Status()}
	type agg struct {
		n     int
		users map[string]bool
		last  int64
		inv   string // son "Invalid user" satırındaki kullanıcı (çift sayımı önlemek için)
	}
	ips := map[string]*agg{}
	end := now.Unix()/bucketSec*bucketSec + bucketSec
	security.SSHScan(since, "Accepted |Failed (password|publickey)|Invalid user", func(e security.SSHEvent) {
		if e.Type == "accepted" {
			rep.Accepted++
			return
		}
		a, ok := ips[e.IP]
		if !ok {
			if len(ips) >= 100000 {
				return
			}
			a = &agg{users: map[string]bool{}}
			ips[e.IP] = a
		}
		// Parola girişi açıksa geçersiz kullanıcı denemesi iki satır üretir:
		// "Invalid user x" ve ardından "Failed password for invalid user x". Tek deneme say.
		if e.Type == "failed" && a.inv != "" && a.inv == e.User {
			a.inv = ""
			return
		}
		a.inv = ""
		if e.Type == "invalid" {
			a.inv = e.User
		}
		rep.Total++
		a.n++
		if len(a.users) < 4 && e.User != "" {
			a.users[e.User] = true
		}
		if t, ok := parseTS(e.TS); ok {
			a.last = t.Unix()
			if i := buckets - 1 - int((end-1-t.Unix())/bucketSec); i >= 0 && i < buckets {
				rep.Hourly[i]++
			}
		}
	})
	rep.UniqueIPs = len(ips)
	banned := map[string]bool{}
	for _, b := range security.Fail2banBanned() {
		banned[b] = true
	}
	byCC := map[string]*countryStat{}
	list := make([]ipStat, 0, len(ips))
	for ip, a := range ips {
		cc := s.geo.Country(ip)
		key := cc
		if key == "" {
			key = "??"
		}
		c, ok := byCC[key]
		if !ok {
			c = &countryStat{CC: key}
			byCC[key] = c
		}
		c.Count += a.n
		c.IPs++
		var us []string
		for u := range a.users {
			us = append(us, u)
		}
		sort.Strings(us)
		list = append(list, ipStat{IP: ip, CC: cc, Count: a.n, Users: strings.Join(us, ", "), Last: a.last, Banned: banned[ip]})
	}
	for _, c := range byCC {
		rep.Countries = append(rep.Countries, *c)
	}
	sort.Slice(rep.Countries, func(i, j int) bool { return rep.Countries[i].Count > rep.Countries[j].Count })
	sort.Slice(list, func(i, j int) bool { return list[i].Count > list[j].Count })
	if len(list) > 50 {
		list = list[:50]
	}
	rep.TopIPs = list
	if rep.Countries == nil {
		rep.Countries = []countryStat{}
	}
	return rep
}

// parseTS journalctl short-iso zamanını okur (sürüme göre +0300 veya +03:00).
func parseTS(s string) (time.Time, bool) {
	for _, l := range []string{"2006-01-02T15:04:05-0700", "2006-01-02T15:04:05-07:00", time.RFC3339Nano} {
		if t, err := time.Parse(l, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
