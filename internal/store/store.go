// Package store panelin verilerini tek bir JSON dosyasında tutar (dış bağımlılık yok).
// Veriler bellekte durur, değişiklikler birkaç saniye içinde diske atomik olarak yazılır.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type User struct {
	Username   string `json:"username"`
	PassHash   string `json:"passHash"`
	TOTPSecret string `json:"totpSecret"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"` // şifre/2FA değişince eski oturumlar geçersiz olur
}

type Session struct {
	TokenHash string `json:"tokenHash"`
	Username  string `json:"username"`
	IP        string `json:"ip"`
	UA        string `json:"ua"`
	CreatedAt int64  `json:"createdAt"`
	LastSeen  int64  `json:"lastSeen"`
	ExpiresAt int64  `json:"expiresAt"`
}

type AuditEntry struct {
	ID       int64  `json:"id"`
	TS       int64  `json:"ts"`
	Username string `json:"username"`
	IP       string `json:"ip"`
	Action   string `json:"action"`
	Target   string `json:"target"`
	OK       bool   `json:"ok"`
	Detail   string `json:"detail"`
}

type Alert struct {
	ID      int64  `json:"id"`
	TS      int64  `json:"ts"`
	Level   string `json:"level"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Sent    bool   `json:"sent"`
}

type MetricPoint struct {
	TS    int64   `json:"ts"`
	CPU   float64 `json:"cpu"`
	Mem   float64 `json:"mem"`
	Swap  float64 `json:"swap"`
	Disk  float64 `json:"disk"`
	Load1 float64 `json:"load1"`
	RX    float64 `json:"rx"`
	TX    float64 `json:"tx"`
}

type data struct {
	Sessions map[string]*Session `json:"sessions"`
	Fails    map[string][]int64  `json:"fails"`
	Audit    []AuditEntry        `json:"audit"`
	Alerts   []Alert             `json:"alerts"`
	Metrics  []MetricPoint       `json:"metrics"`
	KnownIPs map[string]int64    `json:"knownIps"`
	KV       map[string]string   `json:"kv"`
	NextID   int64               `json:"nextId"`
}

const (
	maxAudit   = 5000
	maxAlerts  = 2000
	metricKeep = 7 * 24 * 3600
)

type Store struct {
	mu    sync.Mutex
	path  string
	d     data
	dirty bool
	// Kullanıcılar ayrı dosyada (users.json) durur; panel çalışırken
	// "kullanici-ekle" komutuyla güvenle değiştirilebilsin diye.
	usersPath  string
	users      map[string]*User
	usersMtime time.Time
}

func Now() int64 { return time.Now().Unix() }

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "panel.json"), usersPath: filepath.Join(dir, "users.json"), users: map[string]*User{}}
	b, err := os.ReadFile(s.path)
	if err == nil {
		if err := json.Unmarshal(b, &s.d); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if s.d.Sessions == nil {
		s.d.Sessions = map[string]*Session{}
	}
	if s.d.Fails == nil {
		s.d.Fails = map[string][]int64{}
	}
	if s.d.KnownIPs == nil {
		s.d.KnownIPs = map[string]int64{}
	}
	if s.d.KV == nil {
		s.d.KV = map[string]string{}
	}
	return s, nil
}

// StartFlusher değişiklikleri 3 saniyede bir diske yazar.
func (s *Store) StartFlusher() {
	go func() {
		for {
			time.Sleep(3 * time.Second)
			s.Flush()
		}
	}()
}

// Flush veriyi atomik olarak (geçici dosya + rename) diske yazar.
func (s *Store) Flush() error {
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return nil
	}
	b, err := json.Marshal(s.d)
	s.dirty = false
	s.mu.Unlock()
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) touch() { s.dirty = true }

func (s *Store) nextID() int64 { s.d.NextID++; return s.d.NextID }

// ---- kullanıcılar ----

// loadUsers users.json değiştiyse yeniden okur (s.mu kilitliyken çağrılır).
func (s *Store) loadUsers() {
	fi, err := os.Stat(s.usersPath)
	if err != nil {
		s.users = map[string]*User{}
		return
	}
	if fi.ModTime().Equal(s.usersMtime) {
		return
	}
	b, err := os.ReadFile(s.usersPath)
	if err != nil {
		return
	}
	m := map[string]*User{}
	if json.Unmarshal(b, &m) == nil {
		s.users, s.usersMtime = m, fi.ModTime()
	}
}

func (s *Store) User(username string) (User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadUsers()
	u, ok := s.users[username]
	if !ok {
		return User{}, false
	}
	return *u, true
}

func (s *Store) UpsertUser(u User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadUsers()
	if old, ok := s.users[u.Username]; ok {
		u.CreatedAt = old.CreatedAt
	}
	if u.CreatedAt == 0 {
		u.CreatedAt = Now()
	}
	u.UpdatedAt = Now()
	s.users[u.Username] = &u
	b, err := json.MarshalIndent(s.users, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.usersPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.usersPath)
}

func (s *Store) UserCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadUsers()
	return len(s.users)
}

// ---- oturumlar ----

func (s *Store) AddSession(x Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Sessions[x.TokenHash] = &x
	s.touch()
}

func (s *Store) Session(th string) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, ok := s.d.Sessions[th]
	if !ok {
		return Session{}, false
	}
	return *x, true
}

func (s *Store) TouchSession(th string, ts int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if x, ok := s.d.Sessions[th]; ok {
		x.LastSeen = ts
		s.touch()
	}
}

func (s *Store) DeleteSession(th string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.d.Sessions, th)
	s.touch()
}

// DeleteSessionsExcept verilen dışındaki tüm oturumları siler.
func (s *Store) DeleteSessionsExcept(th string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for k := range s.d.Sessions {
		if k != th {
			delete(s.d.Sessions, k)
			n++
		}
	}
	s.touch()
	return n
}

func (s *Store) DeleteUserSessions(username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, x := range s.d.Sessions {
		if x.Username == username {
			delete(s.d.Sessions, k)
		}
	}
	s.touch()
}

func (s *Store) Sessions() []Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := Now()
	out := []Session{}
	for k, x := range s.d.Sessions {
		if now > x.ExpiresAt {
			delete(s.d.Sessions, k)
			s.touch()
			continue
		}
		out = append(out, *x)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen > out[j].LastSeen })
	return out
}

// ---- hatalı giriş sayacı ----

func (s *Store) FailCount(ip string, since int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, t := range s.d.Fails[ip] {
		if t > since {
			n++
		}
	}
	return n
}

func (s *Store) AddFail(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cut := Now() - 86400
	for k, l := range s.d.Fails { // 1 günden eskileri temizle
		var keep []int64
		for _, t := range l {
			if t > cut {
				keep = append(keep, t)
			}
		}
		if len(keep) == 0 {
			delete(s.d.Fails, k)
		} else {
			s.d.Fails[k] = keep
		}
	}
	s.d.Fails[ip] = append(s.d.Fails[ip], Now())
	s.touch()
}

func (s *Store) ClearFails(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.d.Fails, ip)
	s.touch()
}

// ---- işlem kayıtları ----

func (s *Store) Audit(user, ip, action, target string, ok bool, detail string) {
	if len(detail) > 2000 {
		detail = detail[:2000]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Audit = append(s.d.Audit, AuditEntry{s.nextID(), Now(), user, ip, action, target, ok, detail})
	if len(s.d.Audit) > maxAudit {
		s.d.Audit = s.d.Audit[len(s.d.Audit)-maxAudit:]
	}
	s.touch()
}

func (s *Store) AuditList(limit int) []AuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []AuditEntry{}
	for i := len(s.d.Audit) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.d.Audit[i])
	}
	return out
}

// ---- bildirimler ----

func (s *Store) AddAlert(level, kind, msg string, sent bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Alerts = append(s.d.Alerts, Alert{s.nextID(), Now(), level, kind, msg, sent})
	if len(s.d.Alerts) > maxAlerts {
		s.d.Alerts = s.d.Alerts[len(s.d.Alerts)-maxAlerts:]
	}
	s.touch()
}

func (s *Store) Alerts(limit int) []Alert {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Alert{}
	for i := len(s.d.Alerts) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.d.Alerts[i])
	}
	return out
}

// ---- metrik geçmişi (dakikalık) ----

func (s *Store) AddMetric(p MetricPoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Metrics = append(s.d.Metrics, p)
	cut := Now() - metricKeep
	i := 0
	for i < len(s.d.Metrics) && s.d.Metrics[i].TS < cut {
		i++
	}
	if i > 0 {
		s.d.Metrics = append([]MetricPoint(nil), s.d.Metrics[i:]...)
	}
	s.touch()
}

// Metrics since'den sonraki noktaları en fazla maxPoints olacak şekilde ortalayarak döner.
func (s *Store) Metrics(since int64, maxPoints int) []MetricPoint {
	s.mu.Lock()
	var pts []MetricPoint
	for _, p := range s.d.Metrics {
		if p.TS >= since {
			pts = append(pts, p)
		}
	}
	s.mu.Unlock()
	if len(pts) <= maxPoints {
		if pts == nil {
			return []MetricPoint{}
		}
		return pts
	}
	step := (len(pts) + maxPoints - 1) / maxPoints
	out := make([]MetricPoint, 0, maxPoints)
	for i := 0; i < len(pts); i += step {
		end := i + step
		if end > len(pts) {
			end = len(pts)
		}
		var a MetricPoint
		for _, p := range pts[i:end] {
			a.CPU += p.CPU
			a.Mem += p.Mem
			a.Swap += p.Swap
			a.Disk += p.Disk
			a.Load1 += p.Load1
			a.RX += p.RX
			a.TX += p.TX
		}
		n := float64(end - i)
		a.TS = pts[i].TS
		a.CPU, a.Mem, a.Swap, a.Disk, a.Load1, a.RX, a.TX = a.CPU/n, a.Mem/n, a.Swap/n, a.Disk/n, a.Load1/n, a.RX/n, a.TX/n
		out = append(out, a)
	}
	return out
}

// ---- bilinen SSH IP'leri ----

func (s *Store) IsKnownIP(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.d.KnownIPs[ip]
	return ok
}

func (s *Store) AddKnownIP(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.d.KnownIPs[ip]; !ok {
		s.d.KnownIPs[ip] = Now()
		s.touch()
	}
}

// ---- anahtar/değer ----

func (s *Store) Get(k string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.KV[k]
}

func (s *Store) Set(k, v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.KV[k] = v
	s.touch()
}
