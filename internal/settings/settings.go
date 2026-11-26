// Package settings panelden değiştirilebilen ayarları saklar (settings.json).
//
// İlk açılışta değerler config.env'den (ortam değişkenleri) alınır; sonra panelden
// yapılan her değişiklik bu dosyaya yazılır ve ortam değişkenlerinin yerine geçer.
package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/OsmanxCan/kokpit/internal/config"
	"github.com/OsmanxCan/kokpit/internal/files"
)

type Telegram struct {
	Token    string `json:"token"`
	ChatID   string `json:"chatId"`
	Prefix   bool   `json:"prefix"`   // mesajların başına sunucu adını ekle
	Commands bool   `json:"commands"` // Telegram'dan komut ve buton ile işlem (varsayılan kapalı)
}

type Thresholds struct {
	CPU        float64 `json:"cpu"`        // %
	CPUMinutes int     `json:"cpuMinutes"` // kaç dakika boyunca
	RAM        float64 `json:"ram"`
	RAMMinutes int     `json:"ramMinutes"`
	Disk       float64 `json:"disk"`
	SSHBrute   int     `json:"sshBrute"` // dakikada başarısız SSH denemesi
}

// Event bir bildirim türünün kullanıcı ayarları. Boş Template = varsayılan şablon.
type Event struct {
	Enabled  bool   `json:"enabled"`
	Telegram bool   `json:"telegram"` // kapalıysa sadece panelde listelenir
	Cooldown int    `json:"cooldown"` // dakika
	Template string `json:"template"`
}

type Settings struct {
	Telegram      Telegram         `json:"telegram"`
	Thresholds    Thresholds       `json:"thresholds"`
	Events        map[string]Event `json:"events"`
	WatchServices []string         `json:"watchServices"`
	SessionIPBind bool             `json:"sessionIpBind"`
	UpdateCheck   bool             `json:"updateCheck"`
	FileRoots     []files.Root     `json:"fileRoots"`   // dosya gezgininde erişim izni verilmiş klasörler
	FilePerms     int              `json:"filePerms"`   // izin biçimi sürümü (1 = yazma/silme ayrı)
	TerminalOff   bool             `json:"terminalOff"` // web terminali kapalı
	PortScan      PortScan         `json:"portScan"`
	CFToken       string           `json:"cfToken"` // Cloudflare API token (isteğe bağlı)
	LogRules      []LogRule        `json:"logRules"`
}

// LogRule uygulama logundan IP engelleme kuralı.
type LogRule struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Source    string `json:"source"`    // file | journal
	Path      string `json:"path"`      // dosya yolu veya systemd servis adı
	Pattern   string `json:"pattern"`   // <IP> içeren kalıp
	Regex     bool   `json:"regex"`     // kalıp düzenli ifade mi
	Threshold int    `json:"threshold"` // pencere içinde eşleşme sayısı
	Window    int    `json:"window"`    // dakika
	BanHours  int    `json:"banHours"`  // 0 = kalıcı
	Action    string `json:"action"`    // auto | cloudflare | firewall
	Enabled   bool   `json:"enabled"`
}

// PortScan port tarama tespiti ayarları.
type PortScan struct {
	Off         bool     `json:"off"`         // tespit kapalı (varsayılan açık)
	Threshold   int      `json:"threshold"`   // pencere içinde dokunulan farklı port sayısı
	Window      int      `json:"window"`      // saniye
	AutoBan     bool     `json:"autoBan"`     // hedefli taramaları otomatik engelle (varsayılan kapalı)
	BanInternet bool     `json:"banInternet"` // internet geneli tarayıcıları da engelle
	BanHours    int      `json:"banHours"`    // 0 = kalıcı
	Whitelist   []string `json:"whitelist"`   // asla engellenmeyecek IP / ağlar
}

type Store struct {
	mu   sync.RWMutex
	path string
	s    Settings
}

// Open settings.json'u okur; yoksa config.env değerleriyle oluşturur.
func Open(dir string, cfg *config.Config) (*Store, error) {
	if os.Geteuid() != 0 {
		if abs, err := filepath.Abs(filepath.Join(dir, "depo")); err == nil {
			DefaultStorage = abs
		}
	}
	st := &Store{path: filepath.Join(dir, "settings.json")}
	b, err := os.ReadFile(st.path)
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &st.s); err != nil {
			return nil, err
		}
	case os.IsNotExist(err):
		st.s = Settings{
			Telegram:      Telegram{Token: cfg.TelegramToken, ChatID: cfg.TelegramChat, Prefix: true},
			Thresholds:    Thresholds{CPU: cfg.CPUAlert, CPUMinutes: 3, RAM: cfg.RAMAlert, RAMMinutes: 3, Disk: cfg.DiskAlert, SSHBrute: 30},
			WatchServices: cfg.WatchServices,
			SessionIPBind: cfg.SessionIPBind,
			UpdateCheck:   cfg.UpdateCheck,
		}
	default:
		return nil, err
	}
	st.normalize()
	// root olmadan çalışırken eski varsayılan (/srv) erişilemezse yerel depoya geç
	if DefaultStorage != serverStorage {
		for i, r := range st.s.FileRoots {
			if r.Path == serverStorage {
				if _, e := os.ReadDir(r.Path); e != nil { // yok ya da erişilemiyor
					st.s.FileRoots[i].Path = DefaultStorage
				}
			}
		}
	}
	if os.IsNotExist(err) {
		if err := st.save(); err != nil {
			return nil, err
		}
	}
	return st, nil
}

// normalize eksik alanları varsayılanlarla doldurur (yeni sürümde eklenen olaylar dahil).
func (st *Store) normalize() {
	s := &st.s
	t := &s.Thresholds
	if t.CPU <= 0 {
		t.CPU = 90
	}
	if t.CPUMinutes <= 0 {
		t.CPUMinutes = 3
	}
	if t.RAM <= 0 {
		t.RAM = 90
	}
	if t.RAMMinutes <= 0 {
		t.RAMMinutes = 3
	}
	if t.Disk <= 0 {
		t.Disk = 90
	}
	if t.SSHBrute <= 0 {
		t.SSHBrute = 30
	}
	if s.Events == nil {
		s.Events = map[string]Event{}
	}
	for _, d := range Events {
		if _, ok := s.Events[d.ID]; !ok {
			s.Events[d.ID] = Event{Enabled: d.DefaultOn, Telegram: true, Cooldown: d.Cooldown}
		}
	}
	if s.WatchServices == nil {
		s.WatchServices = []string{}
	}
	if s.LogRules == nil {
		s.LogRules = []LogRule{}
	}
	ps := &s.PortScan
	if ps.Threshold <= 0 {
		ps.Threshold = 15
	}
	if ps.Window <= 0 {
		ps.Window = 60
	}
	if ps.BanHours < 0 {
		ps.BanHours = 24
	}
	if ps.Whitelist == nil {
		ps.Whitelist = []string{}
		if ps.BanHours == 0 && !ps.AutoBan { // ilk kurulum
			ps.BanHours = 24
		}
	}
	if s.FileRoots == nil { // ilk kurulum: sunucuyu depolama alanı olarak kullanmak için bir klasör
		s.FileRoots = []files.Root{{Name: "Depo", Path: DefaultStorage, Write: true, Delete: true}}
		s.FilePerms = 1
	}
	if s.FilePerms < 1 { // eski "salt okunur" biçiminden geçiş
		for i := range s.FileRoots {
			r := &s.FileRoots[i]
			r.Write, r.Delete = !r.ReadOnly, !r.ReadOnly
		}
		s.FilePerms = 1
	}
	for i := range s.FileRoots {
		s.FileRoots[i].ReadOnly = false
	}
}

// DefaultStorage varsayılan depolama klasörü; ilk açılışta yoksa oluşturulur.
// Panel root değilse (örn. geliştirme modunda kendi bilgisayarında) /srv'ye yazamayacağı
// için veri klasörünün içinde bir "depo" klasörü kullanılır.
var DefaultStorage = "/srv/kokpit-depo"

const serverStorage = "/srv/kokpit-depo"

func (st *Store) save() error {
	b, err := json.MarshalIndent(st.s, "", "  ")
	if err != nil {
		return err
	}
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, st.path)
}

// Get ayarların bir kopyasını döner.
func (st *Store) Get() Settings {
	st.mu.RLock()
	defer st.mu.RUnlock()
	c := st.s
	c.Events = make(map[string]Event, len(st.s.Events))
	for k, v := range st.s.Events {
		c.Events[k] = v
	}
	c.WatchServices = append([]string(nil), st.s.WatchServices...)
	c.FileRoots = append([]files.Root{}, st.s.FileRoots...)
	c.PortScan.Whitelist = append([]string{}, st.s.PortScan.Whitelist...)
	c.LogRules = append([]LogRule{}, st.s.LogRules...)
	return c
}

// Update ayarları değiştirir ve diske yazar.
func (st *Store) Update(fn func(s *Settings)) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	fn(&st.s)
	st.normalize()
	return st.save()
}

// Watched izlenen servisleri küme olarak döner.
func (st *Store) Watched() map[string]bool {
	st.mu.RLock()
	defer st.mu.RUnlock()
	m := map[string]bool{}
	for _, w := range st.s.WatchServices {
		m[strings.TrimSuffix(w, ".service")] = true
	}
	return m
}

// MaskToken token'ın sadece başını ve sonunu gösterir.
func MaskToken(t string) string {
	if t == "" {
		return ""
	}
	if len(t) < 12 {
		return "••••"
	}
	return t[:4] + "••••••••" + t[len(t)-4:]
}

// Root adıyla dosya kökünü bulur.
func (st *Store) Root(name string) (files.Root, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	for _, r := range st.s.FileRoots {
		if r.Name == name {
			return r, true
		}
	}
	return files.Root{}, false
}
