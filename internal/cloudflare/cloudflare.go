// Package cloudflare isteğe bağlı Cloudflare entegrasyonu: IP engelleme, "Under Attack"
// modu ve güvenlik olayları. Sadece kullanıcının verdiği API token ile çalışır.
//
// Cloudflare Tünel arkasındaki sitelere gelen istekler sunucuya 127.0.0.1'den geldiği için
// bu sitelere saldıranlar sunucu firewall'unda engellenemez; Cloudflare'de engellenmelidir.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const api = "https://api.cloudflare.com/client/v4"

type Zone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Plan   string `json:"plan"`
	Level  string `json:"securityLevel"` // güvenlik seviyesi (under_attack, medium...)
}

type Ban struct {
	IP      string            `json:"ip"`
	Rules   map[string]string `json:"rules"` // zone id → kural id
	Until   int64             `json:"until"` // 0 = kalıcı
	Created int64             `json:"created"`
	Reason  string            `json:"reason"`
	By      string            `json:"by"`
}

type state struct {
	Bans       []Ban             `json:"bans"`
	PrevLevels map[string]string `json:"prevLevels"` // Under Attack öncesi seviye
}

type Client struct {
	mu    sync.Mutex
	path  string
	st    state
	Token func() string
	API   string // test için
	http  *http.Client

	zmu    sync.Mutex
	zones  []Zone
	zoneAt time.Time
	zerr   error
}

func Open(dataDir string, token func() string) *Client {
	c := &Client{path: filepath.Join(dataDir, "cloudflare.json"), Token: token, API: api, http: &http.Client{Timeout: 20 * time.Second}}
	if b, err := os.ReadFile(c.path); err == nil {
		json.Unmarshal(b, &c.st)
	}
	if c.st.PrevLevels == nil {
		c.st.PrevLevels = map[string]string{}
	}
	return c
}

func (c *Client) saveLocked() {
	b, _ := json.MarshalIndent(c.st, "", "  ")
	tmp := c.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, c.path)
	}
}

func (c *Client) Enabled() bool { return c.Token() != "" }

var ErrNoToken = errors.New("Cloudflare token girilmemiş")

type cfErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// do Cloudflare API isteği yapar; result alanını out'a çözer.
func (c *Client) do(ctx context.Context, token, method, path string, body any, out any) error {
	if token == "" {
		return ErrNoToken
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.API+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("Cloudflare'e ulaşılamadı: %v", err)
	}
	defer resp.Body.Close()
	var r struct {
		Success bool            `json:"success"`
		Errors  []cfErr         `json:"errors"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&r); err != nil {
		return fmt.Errorf("Cloudflare yanıtı okunamadı (HTTP %d)", resp.StatusCode)
	}
	if !r.Success {
		return friendly(resp.StatusCode, r.Errors)
	}
	if out != nil {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

func friendly(status int, errs []cfErr) error {
	msg := ""
	if len(errs) > 0 {
		msg = errs[0].Message
	}
	switch {
	case status == 401 || strings.Contains(msg, "Invalid API Token") || strings.Contains(msg, "Invalid request headers"):
		return errors.New("token geçersiz veya süresi dolmuş")
	case status == 403 || strings.Contains(msg, "Authentication error"):
		return errors.New("token'ın bu işlem için yetkisi yok: " + msg)
	}
	if msg == "" {
		msg = fmt.Sprintf("HTTP %d", status)
	}
	return errors.New("Cloudflare: " + msg)
}

// Verify token'ı doğrular ve erişilebilen alan adlarını döner.
func (c *Client) Verify(ctx context.Context, token string) ([]Zone, error) {
	var v struct {
		Status string `json:"status"`
	}
	if err := c.do(ctx, token, "GET", "/user/tokens/verify", nil, &v); err != nil {
		return nil, err
	}
	if v.Status != "active" {
		return nil, errors.New("token aktif değil (" + v.Status + ")")
	}
	zs, err := c.listZones(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("alan adları okunamadı: %v (token'a Zone:Read yetkisi ver)", err)
	}
	if len(zs) == 0 {
		return nil, errors.New("token hiçbir alan adına erişemiyor; Zone Resources kısmında alan adını seç")
	}
	return zs, nil
}

func (c *Client) listZones(ctx context.Context, token string) ([]Zone, error) {
	var raw []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
		Plan   struct {
			Name string `json:"name"`
		} `json:"plan"`
	}
	if err := c.do(ctx, token, "GET", "/zones?per_page=50", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]Zone, 0, len(raw))
	for _, z := range raw {
		out = append(out, Zone{ID: z.ID, Name: z.Name, Status: z.Status, Plan: z.Plan.Name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Zones alan adlarını ve güvenlik seviyelerini döner (1 dakika önbellekli).
func (c *Client) Zones(ctx context.Context, fresh bool) ([]Zone, error) {
	c.zmu.Lock()
	defer c.zmu.Unlock()
	if !fresh && time.Since(c.zoneAt) < time.Minute && (c.zones != nil || c.zerr != nil) {
		return c.zones, c.zerr
	}
	token := c.Token()
	zs, err := c.listZones(ctx, token)
	if err == nil {
		for i := range zs {
			var s struct {
				Value string `json:"value"`
			}
			if c.do(ctx, token, "GET", "/zones/"+zs[i].ID+"/settings/security_level", nil, &s) == nil {
				zs[i].Level = s.Value
			}
		}
	}
	c.zones, c.zerr, c.zoneAt = zs, err, time.Now()
	return zs, err
}

func (c *Client) zoneIDs(ctx context.Context) ([]string, error) {
	zs, err := c.Zones(ctx, false)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(zs))
	for _, z := range zs {
		ids = append(ids, z.ID)
	}
	return ids, nil
}

// ---------- IP engelleme ----------

// BanIP IP'yi tokenın eriştiği tüm alan adlarında engeller (IP Access Rules).
func (c *Client) BanIP(ip, reason string, hours int, by string) error {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return errors.New("geçersiz IP")
	}
	c.mu.Lock()
	for _, b := range c.st.Bans {
		if b.IP == ip {
			c.mu.Unlock()
			return nil
		}
	}
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ids, err := c.zoneIDs(ctx)
	if err != nil {
		return err
	}
	target := "ip"
	if parsed.To4() == nil {
		target = "ip6"
	}
	b := Ban{IP: ip, Rules: map[string]string{}, Created: time.Now().Unix(), Reason: reason, By: by}
	if hours > 0 {
		b.Until = time.Now().Add(time.Duration(hours) * time.Hour).Unix()
	}
	var lastErr error
	for _, z := range ids {
		var rule struct {
			ID string `json:"id"`
		}
		body := map[string]any{"mode": "block", "configuration": map[string]string{"target": target, "value": ip},
			"notes": "Kokpit: " + truncate(reason, 80)}
		if err := c.do(ctx, c.Token(), "POST", "/zones/"+z+"/firewall/access_rules/rules", body, &rule); err != nil {
			lastErr = err
			continue
		}
		b.Rules[z] = rule.ID
	}
	if len(b.Rules) == 0 {
		if lastErr == nil {
			lastErr = errors.New("hiçbir alan adında engellenemedi")
		}
		return fmt.Errorf("%v (token'a Zone → Firewall Services: Edit yetkisi ver)", lastErr)
	}
	c.mu.Lock()
	c.st.Bans = append(c.st.Bans, b)
	c.saveLocked()
	c.mu.Unlock()
	return nil
}

func (c *Client) UnbanIP(ip string) error {
	c.mu.Lock()
	var b *Ban
	for i := range c.st.Bans {
		if c.st.Bans[i].IP == ip {
			cp := c.st.Bans[i]
			b = &cp
		}
	}
	c.mu.Unlock()
	if b == nil {
		return errors.New(ip + " Kokpit tarafından Cloudflare'de engellenmemiş")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var lastErr error
	for z, id := range b.Rules {
		if err := c.do(ctx, c.Token(), "DELETE", "/zones/"+z+"/firewall/access_rules/rules/"+id, nil, nil); err != nil &&
			!strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "could not be found") {
			lastErr = err
		}
	}
	if lastErr != nil {
		return lastErr
	}
	c.mu.Lock()
	keep := c.st.Bans[:0]
	for _, x := range c.st.Bans {
		if x.IP != ip {
			keep = append(keep, x)
		}
	}
	c.st.Bans = keep
	c.saveLocked()
	c.mu.Unlock()
	return nil
}

func (c *Client) Bans() []Ban {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Ban{}, c.st.Bans...)
}

// Banned IP Kokpit tarafından Cloudflare'de engellenmiş mi.
func (c *Client) Banned(ip string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, b := range c.st.Bans {
		if b.IP == ip {
			return true
		}
	}
	return false
}

// Expire süresi dolan engelleri kaldırır.
func (c *Client) Expire() {
	if !c.Enabled() {
		return
	}
	now := time.Now().Unix()
	for _, b := range c.Bans() {
		if b.Until > 0 && now >= b.Until {
			c.UnbanIP(b.IP)
		}
	}
}

func (c *Client) Start() {
	go func() {
		for range time.Tick(time.Minute) {
			c.Expire()
		}
	}()
}

// ---------- Under Attack ----------

// SetUnderAttack alan adında "I'm Under Attack" modunu açar/kapatır. Kapatırken önceki
// güvenlik seviyesine döner.
func (c *Client) SetUnderAttack(zoneID string, on bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ids, err := c.zoneIDs(ctx)
	if err != nil {
		return err
	}
	ok := false
	for _, z := range ids {
		if z == zoneID {
			ok = true
		}
	}
	if !ok {
		return errors.New("alan adı bulunamadı")
	}
	var cur struct {
		Value string `json:"value"`
	}
	if err := c.do(ctx, c.Token(), "GET", "/zones/"+zoneID+"/settings/security_level", nil, &cur); err != nil {
		return err
	}
	val := "under_attack"
	if !on {
		c.mu.Lock()
		val = c.st.PrevLevels[zoneID]
		c.mu.Unlock()
		if val == "" || val == "under_attack" {
			val = "medium"
		}
	} else if cur.Value != "under_attack" {
		c.mu.Lock()
		c.st.PrevLevels[zoneID] = cur.Value
		c.saveLocked()
		c.mu.Unlock()
	}
	if err := c.do(ctx, c.Token(), "PATCH", "/zones/"+zoneID+"/settings/security_level", map[string]string{"value": val}, nil); err != nil {
		return fmt.Errorf("%v (token'a Zone → Zone Settings: Edit yetkisi ver)", err)
	}
	c.zmu.Lock()
	c.zoneAt = time.Time{}
	c.zmu.Unlock()
	return nil
}

// ---------- güvenlik olayları ----------

type Event struct {
	Time    string `json:"time"`
	Action  string `json:"action"`
	IP      string `json:"ip"`
	Country string `json:"country"`
	Host    string `json:"host"`
	Path    string `json:"path"`
	Source  string `json:"source"`
	UA      string `json:"ua"`
}

const eventsQuery = `query($zone: String!, $since: Time!) {
  viewer { zones(filter: {zoneTag: $zone}) {
    firewallEventsAdaptive(filter: {datetime_geq: $since}, limit: 100, orderBy: [datetime_DESC]) {
      action clientIP clientCountryName clientRequestHTTPHost clientRequestPath datetime source userAgent
    }
  } }
}`

// Events son 24 saatte Cloudflare'in engellediği veya sorguladığı istekleri döner.
// Planın izin vermediği durumda açıklayıcı bir hata döner.
func (c *Client) Events(zoneID string) ([]Event, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]any{"query": eventsQuery, "variables": map[string]string{
		"zone": zoneID, "since": time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)}})
	req, _ := http.NewRequestWithContext(ctx, "POST", c.API+"/graphql", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+c.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Cloudflare'e ulaşılamadı: %v", err)
	}
	defer resp.Body.Close()
	var r struct {
		Data struct {
			Viewer struct {
				Zones []struct {
					Events []struct {
						Action  string `json:"action"`
						IP      string `json:"clientIP"`
						Country string `json:"clientCountryName"`
						Host    string `json:"clientRequestHTTPHost"`
						Path    string `json:"clientRequestPath"`
						Time    string `json:"datetime"`
						Source  string `json:"source"`
						UA      string `json:"userAgent"`
					} `json:"firewallEventsAdaptive"`
				} `json:"zones"`
			} `json:"viewer"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&r); err != nil {
		return nil, fmt.Errorf("Cloudflare yanıtı okunamadı (HTTP %d)", resp.StatusCode)
	}
	if len(r.Errors) > 0 {
		m := r.Errors[0].Message
		if strings.Contains(m, "not authorized") || strings.Contains(m, "does not have access") {
			return nil, errors.New("token'a Zone → Analytics: Read yetkisi gerekiyor")
		}
		return nil, errors.New("Cloudflare: " + m)
	}
	out := []Event{}
	for _, z := range r.Data.Viewer.Zones {
		for _, e := range z.Events {
			out = append(out, Event{Time: e.Time, Action: e.Action, IP: e.IP, Country: e.Country, Host: e.Host, Path: e.Path, Source: e.Source, UA: e.UA})
		}
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ---------- DNS ----------

// zoneFor alan adının ait olduğu bölgeyi (en uzun eşleşen) bulur.
func (c *Client) zoneFor(ctx context.Context, host string) (Zone, error) {
	zs, err := c.Zones(ctx, false)
	if err != nil {
		return Zone{}, err
	}
	var best Zone
	for _, z := range zs {
		if (host == z.Name || strings.HasSuffix(host, "."+z.Name)) && len(z.Name) > len(best.Name) {
			best = z
		}
	}
	if best.ID == "" {
		return Zone{}, fmt.Errorf("%s, token'ın eriştiği alan adlarından birine ait değil", host)
	}
	return best, nil
}

type dnsRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
}

// EnsureCNAME host için target'a giden, Cloudflare proxy'li bir CNAME kaydı oluşturur.
// Aynı kayıt zaten varsa dokunmaz (created=false). Başka bir kayıt varsa hata döner.
func (c *Client) EnsureCNAME(host, target, comment string) (zoneID, recordID string, created bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	z, err := c.zoneFor(ctx, host)
	if err != nil {
		return "", "", false, err
	}
	var existing []dnsRecord
	if err := c.do(ctx, c.Token(), "GET", "/zones/"+z.ID+"/dns_records?name="+host, nil, &existing); err != nil {
		return "", "", false, fmt.Errorf("%v (token'a Zone → DNS: Edit yetkisi ver)", err)
	}
	for _, r := range existing {
		if r.Type == "CNAME" && strings.EqualFold(r.Content, target) {
			return z.ID, r.ID, false, nil
		}
	}
	if len(existing) > 0 {
		return "", "", false, fmt.Errorf("%s için zaten bir DNS kaydı var (%s → %s); üzerine yazılmadı", host, existing[0].Type, existing[0].Content)
	}
	var rec dnsRecord
	body := map[string]any{"type": "CNAME", "name": host, "content": target, "proxied": true, "ttl": 1, "comment": truncate(comment, 90)}
	if err := c.do(ctx, c.Token(), "POST", "/zones/"+z.ID+"/dns_records", body, &rec); err != nil {
		return "", "", false, fmt.Errorf("%v (token'a Zone → DNS: Edit yetkisi ver)", err)
	}
	return z.ID, rec.ID, true, nil
}

// DeleteRecord DNS kaydını siler.
func (c *Client) DeleteRecord(zoneID, recordID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err := c.do(ctx, c.Token(), "DELETE", "/zones/"+zoneID+"/dns_records/"+recordID, nil, nil)
	if err != nil && (strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "could not be found")) {
		return nil
	}
	return err
}
