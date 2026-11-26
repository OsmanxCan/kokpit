// Package deploy projeleri tek tıkla günceller: git'ten son sürümü çeker, tanımlı
// adımları (bağımlılık kurulumu, derleme…) çalıştırır, PM2 uygulamasını yeniden başlatır
// ve isteğe bağlı sağlık kontrolü yapar. Hata olursa önceki sürüme geri dönebilir.
package deploy

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

type Step struct {
	Name string `json:"name"`
	Cmd  string `json:"cmd"`
}

type Project struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Dir          string `json:"dir"`
	Branch       string `json:"branch"`       // boş = git kullanma (sadece adımlar)
	Steps        []Step `json:"steps"`        // git'ten sonra sırayla çalışır
	PM2          string `json:"pm2"`          // yeniden başlatılacak PM2 uygulaması (isteğe bağlı)
	HealthURL    string `json:"healthUrl"`    // deploy sonrası kontrol edilecek adres (isteğe bağlı)
	AutoRollback bool   `json:"autoRollback"` // hata olursa önceki sürüme dön
	Telegram     bool   `json:"telegram"`     // Telegram'dan deploy edilebilir
	RunAs        string `json:"runAs"`        // komutları çalıştıracak kullanıcı; boş = klasörün sahibi
	CreatedAt    int64  `json:"createdAt"`
}

type Run struct {
	ID       string `json:"id"`
	Project  string `json:"project"`
	Status   string `json:"status"`  // running | ok | failed | rolled_back | canceled
	Trigger  string `json:"trigger"` // panel | telegram
	User     string `json:"user"`
	Start    int64  `json:"start"`
	End      int64  `json:"end"`
	From     string `json:"from"`    // önceki commit
	To       string `json:"to"`      // yeni commit
	Message  string `json:"message"` // yeni commit mesajı
	Target   string `json:"target"`  // geri alma için istenen commit ("" = en yeni)
	Error    string `json:"error,omitempty"`
	StepName string `json:"step"` // şu an çalışan / hata veren adım
}

type data struct {
	Projects []Project `json:"projects"`
	Runs     []Run     `json:"runs"` // en yeniler sonda
}

// Event bir deploy bittiğinde bildirim için.
type Event struct {
	Project Project
	Run     Run
}

type Engine struct {
	mu       sync.Mutex
	dir      string
	path     string
	d        data
	live     map[string]*liveRun // çalışan deploy'lar (run id → canlı log)
	busy     map[string]string   // proje id → çalışan run id
	OnFinish func(Event)
	Restart  func(pm2 string) error // PM2 uygulamasını yeniden başlatır
}

func Open(dataDir string) (*Engine, error) {
	e := &Engine{dir: filepath.Join(dataDir, "deploy"), path: filepath.Join(dataDir, "deploy.json"),
		live: map[string]*liveRun{}, busy: map[string]string{}}
	if err := os.MkdirAll(e.dir, 0o700); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(e.path)
	if err == nil {
		if err := json.Unmarshal(b, &e.d); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if e.d.Projects == nil {
		e.d.Projects = []Project{}
	}
	// panel kapanırken yarıda kalanları işaretle
	for i := range e.d.Runs {
		if e.d.Runs[i].Status == "running" {
			e.d.Runs[i].Status, e.d.Runs[i].Error = "failed", "panel yeniden başlatıldığı için yarıda kaldı"
		}
	}
	return e, nil
}

func (e *Engine) saveLocked() {
	b, err := json.MarshalIndent(e.d, "", "  ")
	if err != nil {
		return
	}
	tmp := e.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, e.path)
	}
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------- proje yönetimi ----------

func Validate(p *Project) error {
	p.Name = strings.TrimSpace(p.Name)
	p.Dir = filepath.Clean(strings.TrimSpace(p.Dir))
	p.Branch = strings.TrimSpace(p.Branch)
	p.PM2 = strings.TrimSpace(p.PM2)
	p.HealthURL = strings.TrimSpace(p.HealthURL)
	p.RunAs = strings.TrimSpace(p.RunAs)
	if p.Name == "" || utf8.RuneCountInString(p.Name) > 50 {
		return errors.New("proje adı 1–50 karakter olmalı")
	}
	if !filepath.IsAbs(p.Dir) || p.Dir == "/" {
		return errors.New("proje klasörü tam bir yol olmalı (örn. /var/www/uygulama)")
	}
	if fi, err := os.Stat(p.Dir); err != nil || !fi.IsDir() {
		return errors.New(p.Dir + " bulunamadı veya bir klasör değil")
	}
	if p.Branch != "" {
		if !safeRef(p.Branch) {
			return errors.New("dal adı geçersiz")
		}
		if _, err := os.Stat(filepath.Join(p.Dir, ".git")); err != nil {
			return errors.New(p.Dir + " bir git deposu değil; dal alanını boş bırakırsan sadece adımlar çalışır")
		}
	}
	if len(p.Steps) > 20 {
		return errors.New("en fazla 20 adım eklenebilir")
	}
	steps := p.Steps[:0]
	for _, s := range p.Steps {
		s.Name, s.Cmd = strings.TrimSpace(s.Name), strings.TrimSpace(s.Cmd)
		if s.Cmd == "" {
			continue
		}
		if len(s.Cmd) > 2000 {
			return errors.New("bir adımın komutu en fazla 2000 karakter olabilir")
		}
		if s.Name == "" {
			s.Name = s.Cmd
			if len(s.Name) > 40 {
				s.Name = s.Name[:40] + "…"
			}
		}
		steps = append(steps, s)
	}
	p.Steps = steps
	if p.PM2 != "" && !safeName(p.PM2) {
		return errors.New("PM2 uygulama adı geçersiz")
	}
	if p.HealthURL != "" {
		u, err := url.Parse(p.HealthURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("sağlık kontrolü adresi http:// veya https:// ile başlamalı")
		}
	}
	if p.RunAs != "" {
		if _, err := user.Lookup(p.RunAs); err != nil {
			return errors.New(p.RunAs + " adında bir kullanıcı yok")
		}
	}
	if p.Branch == "" && len(p.Steps) == 0 && p.PM2 == "" {
		return errors.New("en az bir şey yapılmalı: git dalı, bir adım veya PM2 uygulaması")
	}
	return nil
}

func safeRef(s string) bool {
	if s == "" || len(s) > 100 || strings.HasPrefix(s, "-") || strings.Contains(s, "..") {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_./", c)) {
			return false
		}
	}
	return true
}

func safeName(s string) bool {
	if s == "" || len(s) > 100 || strings.HasPrefix(s, "-") {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.@", c)) {
			return false
		}
	}
	return true
}

func isCommit(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	_, err := hex.DecodeString(s + strings.Repeat("0", len(s)%2))
	return err == nil
}

func (e *Engine) Projects() []Project {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Project{}, e.d.Projects...)
}

func (e *Engine) Project(id string) (Project, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, p := range e.d.Projects {
		if p.ID == id {
			return p, true
		}
	}
	return Project{}, false
}

func (e *Engine) Save(p Project) (Project, error) {
	if err := Validate(&p); err != nil {
		return p, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if p.ID == "" {
		if len(e.d.Projects) >= 30 {
			return p, errors.New("en fazla 30 proje eklenebilir")
		}
		p.ID, p.CreatedAt = newID(), time.Now().Unix()
		e.d.Projects = append(e.d.Projects, p)
	} else {
		found := false
		for i, x := range e.d.Projects {
			if x.ID == p.ID {
				p.CreatedAt = x.CreatedAt
				e.d.Projects[i] = p
				found = true
			}
		}
		if !found {
			return p, errors.New("proje bulunamadı")
		}
	}
	e.saveLocked()
	return p, nil
}

func (e *Engine) Delete(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.busy[id]; ok {
		return errors.New("deploy sürerken proje silinemez")
	}
	for i, x := range e.d.Projects {
		if x.ID == id {
			e.d.Projects = append(e.d.Projects[:i], e.d.Projects[i+1:]...)
			runs := e.d.Runs[:0]
			for _, r := range e.d.Runs {
				if r.Project == id {
					os.Remove(filepath.Join(e.dir, r.ID+".log"))
					continue
				}
				runs = append(runs, r)
			}
			e.d.Runs = runs
			e.saveLocked()
			return nil
		}
	}
	return errors.New("proje bulunamadı")
}

// Runs projenin geçmişini döner (en yeni başta). id boşsa tüm projeler.
func (e *Engine) Runs(project string, limit int) []Run {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []Run{}
	for i := len(e.d.Runs) - 1; i >= 0 && len(out) < limit; i-- {
		if project == "" || e.d.Runs[i].Project == project {
			out = append(out, e.d.Runs[i])
		}
	}
	return out
}

func (e *Engine) Run(id string) (Run, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range e.d.Runs {
		if r.ID == id {
			return r, true
		}
	}
	return Run{}, false
}

// Busy projede çalışan deploy varsa id'sini döner.
func (e *Engine) Busy(project string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.busy[project]
}

// ---------- canlı log ----------

type liveRun struct {
	mu     sync.Mutex
	buf    []byte
	done   bool
	notify chan struct{}
	cancel context.CancelFunc
	f      *os.File
}

const maxLog = 4 << 20

func (l *liveRun) Write(p []byte) (int, error) {
	l.mu.Lock()
	if len(l.buf) < maxLog {
		l.buf = append(l.buf, p...)
		if l.f != nil {
			l.f.Write(p)
		}
	}
	ch := l.notify
	l.notify = make(chan struct{})
	l.mu.Unlock()
	close(ch)
	return len(p), nil
}

func (l *liveRun) line(format string, a ...any) {
	l.Write([]byte(fmt.Sprintf(format, a...) + "\n"))
}

// Follow log'u baştan itibaren akıtır; deploy bitince döner. Canlı değilse dosyadan okur.
func (e *Engine) Follow(ctx context.Context, runID string, fn func([]byte) error) error {
	e.mu.Lock()
	l := e.live[runID]
	e.mu.Unlock()
	if l == nil {
		f, err := os.Open(filepath.Join(e.dir, runID+".log"))
		if err != nil {
			return errors.New("bu deploy'un kaydı bulunamadı")
		}
		defer f.Close()
		b, _ := io.ReadAll(io.LimitReader(f, maxLog))
		return fn(b)
	}
	off := 0
	for {
		l.mu.Lock()
		chunk := append([]byte(nil), l.buf[off:]...)
		off = len(l.buf)
		done, ch := l.done, l.notify
		l.mu.Unlock()
		if len(chunk) > 0 {
			if err := fn(chunk); err != nil {
				return err
			}
		}
		if done {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		case <-time.After(15 * time.Second):
			if err := fn(nil); err != nil { // canlı tutma
				return err
			}
		}
	}
}

// Cancel çalışan deploy'u durdurur.
func (e *Engine) Cancel(runID string) error {
	e.mu.Lock()
	l := e.live[runID]
	e.mu.Unlock()
	if l == nil {
		return errors.New("çalışan bir deploy yok")
	}
	l.line("\n■ Kullanıcı tarafından durduruldu")
	l.cancel()
	return nil
}

// ---------- çalıştırma ----------

type Options struct {
	Target  string // geri alma: bu commit'e dön ("" = dalın en yenisi)
	Trigger string // panel | telegram
	User    string
}

var ErrBusy = errors.New("bu projede zaten bir deploy sürüyor")

// Start deploy'u arka planda başlatır ve run kimliğini döner.
func (e *Engine) Start(projectID string, o Options) (Run, error) {
	if o.Target != "" && !isCommit(o.Target) {
		return Run{}, errors.New("geçersiz commit")
	}
	e.mu.Lock()
	var p *Project
	for i := range e.d.Projects {
		if e.d.Projects[i].ID == projectID {
			cp := e.d.Projects[i]
			p = &cp
		}
	}
	if p == nil {
		e.mu.Unlock()
		return Run{}, errors.New("proje bulunamadı")
	}
	if _, ok := e.busy[projectID]; ok {
		e.mu.Unlock()
		return Run{}, ErrBusy
	}
	if o.Target != "" && p.Branch == "" {
		e.mu.Unlock()
		return Run{}, errors.New("git kullanmayan proje geri alınamaz")
	}
	r := Run{ID: newID() + strconv.FormatInt(time.Now().Unix()%100000, 36), Project: p.ID, Status: "running", Trigger: o.Trigger, User: o.User,
		Start: time.Now().Unix(), Target: o.Target}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	l := &liveRun{notify: make(chan struct{}), cancel: cancel}
	l.f, _ = os.OpenFile(filepath.Join(e.dir, r.ID+".log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	e.live[r.ID] = l
	e.busy[p.ID] = r.ID
	e.d.Runs = append(e.d.Runs, r)
	e.trimLocked()
	e.saveLocked()
	e.mu.Unlock()

	go e.execute(ctx, *p, r, l)
	return r, nil
}

// trimLocked her projenin en fazla 30 kaydını tutar.
func (e *Engine) trimLocked() {
	count := map[string]int{}
	keep := make([]Run, 0, len(e.d.Runs))
	for i := len(e.d.Runs) - 1; i >= 0; i-- {
		r := e.d.Runs[i]
		count[r.Project]++
		if count[r.Project] > 30 {
			os.Remove(filepath.Join(e.dir, r.ID+".log"))
			continue
		}
		keep = append(keep, r)
	}
	sort.SliceStable(keep, func(i, j int) bool { return keep[i].Start < keep[j].Start })
	e.d.Runs = keep
}

func (e *Engine) update(r Run) {
	e.mu.Lock()
	for i := range e.d.Runs {
		if e.d.Runs[i].ID == r.ID {
			e.d.Runs[i] = r
		}
	}
	e.saveLocked()
	e.mu.Unlock()
}

type runner struct {
	p    Project
	l    *liveRun
	cred *syscall.Credential
	env  []string
}

func (e *Engine) execute(ctx context.Context, p Project, r Run, l *liveRun) {
	defer func() {
		r.End = time.Now().Unix()
		l.line("")
		switch r.Status {
		case "ok":
			l.line("✓ Deploy tamamlandı (%s)", time.Duration(r.End-r.Start)*time.Second)
		case "rolled_back":
			l.line("↩ Deploy başarısız oldu, önceki sürüme geri dönüldü")
		case "canceled":
			l.line("■ Deploy durduruldu")
		default:
			l.line("✗ Deploy başarısız: %s", r.Error)
		}
		// Sonucu kaydetmek ve projeyi serbest bırakmak tek adımda yapılır; böylece durum
		// "bitti" görünür görünmez yeni bir deploy başlatılabilir.
		e.mu.Lock()
		for i := range e.d.Runs {
			if e.d.Runs[i].ID == r.ID {
				e.d.Runs[i] = r
			}
		}
		delete(e.busy, p.ID)
		delete(e.live, r.ID)
		e.saveLocked()
		e.mu.Unlock()
		l.mu.Lock()
		l.done = true
		if l.f != nil {
			l.f.Close()
		}
		ch := l.notify
		l.notify = make(chan struct{})
		l.mu.Unlock()
		close(ch)
		l.cancel()
		if e.OnFinish != nil {
			e.OnFinish(Event{Project: p, Run: r})
		}
	}()

	rn := &runner{p: p, l: l}
	if err := rn.setup(); err != nil {
		r.Status, r.Error = "failed", err.Error()
		return
	}
	l.line("▶ %s deploy ediliyor · %s", p.Name, time.Now().Format("02.01.2006 15:04:05"))
	l.line("  klasör: %s · kullanıcı: %s", p.Dir, rn.who())

	// 1) git
	if p.Branch != "" {
		r.StepName = "git"
		e.update(r)
		from, to, msg, err := rn.git(ctx, r.Target)
		r.From, r.To, r.Message = from, to, msg
		e.update(r)
		if err != nil {
			r.Status, r.Error = failStatus(ctx), err.Error()
			return
		}
		if from == to && r.Target == "" {
			l.line("  zaten en yeni sürüm (%s); adımlar yine de çalıştırılıyor", short(to))
		}
	}

	// 2) adımlar + yeniden başlatma + sağlık kontrolü
	if err := e.build(ctx, rn, &r); err != nil {
		r.Error = err.Error()
		if ctx.Err() != nil {
			r.Status = failStatus(ctx)
			return
		}
		r.Status = "failed"
		if p.AutoRollback && p.Branch != "" && r.From != "" && r.From != r.To {
			l.line("")
			l.line("↩ Otomatik geri alma: %s sürümüne dönülüyor", short(r.From))
			rctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
			defer cancel()
			if _, _, _, gerr := rn.git(rctx, r.From); gerr != nil {
				l.line("  geri alma başarısız: %v", gerr)
				return
			}
			tmp := r
			if berr := e.build(rctx, rn, &tmp); berr != nil {
				l.line("  geri alma sonrası adımlar başarısız: %v", berr)
				return
			}
			r.Status = "rolled_back"
		}
		return
	}
	r.Status, r.StepName = "ok", ""
}

func failStatus(ctx context.Context) string {
	if errors.Is(ctx.Err(), context.Canceled) {
		return "canceled"
	}
	return "failed"
}

func (e *Engine) build(ctx context.Context, rn *runner, r *Run) error {
	for i, s := range rn.p.Steps {
		r.StepName = s.Name
		e.update(*r)
		rn.l.line("")
		rn.l.line("▶ [%d/%d] %s", i+1, len(rn.p.Steps), s.Name)
		rn.l.line("$ %s", s.Cmd)
		t0 := time.Now()
		if err := rn.shell(ctx, s.Cmd); err != nil {
			rn.l.line("✗ %v", err)
			return fmt.Errorf("%s adımı başarısız: %v", s.Name, err)
		}
		if d := time.Since(t0); d > 2*time.Second {
			rn.l.line("  %s sürdü", d.Round(time.Second))
		}
	}
	if rn.p.PM2 != "" {
		r.StepName = "pm2"
		e.update(*r)
		rn.l.line("")
		rn.l.line("▶ PM2: %s yeniden başlatılıyor", rn.p.PM2)
		if e.Restart == nil {
			return errors.New("PM2 kullanılamıyor")
		}
		if err := e.Restart(rn.p.PM2); err != nil {
			return fmt.Errorf("PM2 yeniden başlatılamadı: %v", err)
		}
		rn.l.line("  tamam")
	}
	if rn.p.HealthURL != "" {
		r.StepName = "sağlık kontrolü"
		e.update(*r)
		rn.l.line("")
		rn.l.line("▶ Sağlık kontrolü: %s", rn.p.HealthURL)
		if err := health(ctx, rn.p.HealthURL, rn.l); err != nil {
			return err
		}
	}
	return nil
}

func health(ctx context.Context, u string, l *liveRun) error {
	cl := &http.Client{Timeout: 10 * time.Second}
	var last string
	for i := 0; i < 10; i++ { // uygulamanın açılması için ~40 saniye bekle
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(4 * time.Second):
			}
		}
		req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
		req.Header.Set("User-Agent", "Kokpit-Deploy")
		resp, err := cl.Do(req)
		if err != nil {
			last = err.Error()
			l.line("  deneme %d: %v", i+1, err)
			continue
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if resp.StatusCode < 400 {
			l.line("  HTTP %d · site yanıt veriyor", resp.StatusCode)
			return nil
		}
		last = fmt.Sprintf("HTTP %d", resp.StatusCode)
		l.line("  deneme %d: %s", i+1, last)
	}
	return fmt.Errorf("sağlık kontrolü başarısız (%s)", last)
}

// setup komutları çalıştıracak kullanıcıyı belirler: RunAs veya klasörün sahibi.
func (rn *runner) setup() error {
	var u *user.User
	var err error
	if rn.p.RunAs != "" {
		u, err = user.Lookup(rn.p.RunAs)
	} else {
		fi, e := os.Stat(rn.p.Dir)
		if e != nil {
			return fmt.Errorf("proje klasörü açılamadı: %v", e)
		}
		st := fi.Sys().(*syscall.Stat_t)
		u, err = user.LookupId(strconv.Itoa(int(st.Uid)))
	}
	if err != nil {
		return fmt.Errorf("kullanıcı bulunamadı: %v", err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	if uid != os.Geteuid() {
		if os.Geteuid() != 0 {
			return fmt.Errorf("komutlar %s olarak çalıştırılamaz (panel root değil)", u.Username)
		}
		var groups []uint32
		if gs, err := u.GroupIds(); err == nil {
			for _, g := range gs {
				if n, err := strconv.Atoi(g); err == nil {
					groups = append(groups, uint32(n))
				}
			}
		}
		rn.cred = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: groups}
	}
	path := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/snap/bin"
	// nvm ile kurulmuş node/npm'i bul
	if nodes, _ := filepath.Glob(filepath.Join(u.HomeDir, ".nvm/versions/node/*/bin")); len(nodes) > 0 {
		sort.Strings(nodes)
		path = nodes[len(nodes)-1] + ":" + path
	}
	rn.env = []string{"HOME=" + u.HomeDir, "USER=" + u.Username, "LOGNAME=" + u.Username, "PATH=" + path,
		"LANG=C.UTF-8", "CI=true", "FORCE_COLOR=0", "NO_COLOR=1", "GIT_TERMINAL_PROMPT=0", "KOKPIT_DEPLOY=1"}
	return nil
}

func (rn *runner) who() string {
	for _, e := range rn.env {
		if strings.HasPrefix(e, "USER=") {
			return e[5:]
		}
	}
	return "?"
}

func (rn *runner) command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = rn.p.Dir
	cmd.Env = rn.env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Credential: rn.cred}
	// iptalde tüm süreç grubunu durdur (npm'in alt süreçleri dahil)
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 10 * time.Second
	return cmd
}

// shell bir adımı bash ile çalıştırır ve çıktısını canlı loga yazar.
func (rn *runner) shell(ctx context.Context, script string) error {
	sctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	cmd := rn.command(sctx, "/bin/bash", "-o", "pipefail", "-c", script)
	return rn.stream(cmd)
}

func (rn *runner) stream(cmd *exec.Cmd) error {
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			rn.l.Write(append(stripCR(sc.Bytes()), '\n'))
		}
		io.Copy(io.Discard, pr)
		close(done)
	}()
	err := cmd.Run()
	pw.Close()
	<-done
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() >= 0 {
			return fmt.Errorf("çıkış kodu %d", ee.ExitCode())
		}
		if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
			return errors.New("durduruldu veya zaman aşımı")
		}
	}
	return err
}

// stripCR ilerleme çubuklarının \r ile ürettiği satırlardan sonuncusunu tutar.
func stripCR(b []byte) []byte {
	if i := strings.LastIndexByte(strings.TrimRight(string(b), "\r"), '\r'); i >= 0 {
		return b[i+1:]
	}
	return b
}

func (rn *runner) gitOut(ctx context.Context, args ...string) (string, error) {
	a := append([]string{"-c", "safe.directory=" + rn.p.Dir}, args...)
	out, err := rn.command(ctx, "git", a...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// git yerel değişiklik olmadığını doğrular, uzaktan çeker ve target'a (boşsa dalın en yenisine) geçer.
func (rn *runner) git(ctx context.Context, target string) (from, to, msg string, err error) {
	l := rn.l
	l.line("")
	from, err = rn.gitOut(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", "", "", fmt.Errorf("git deposu okunamadı: %s", from)
	}
	if st, _ := rn.gitOut(ctx, "status", "--porcelain", "--untracked-files=no"); st != "" {
		l.line("✗ Klasörde commit edilmemiş değişiklikler var:")
		l.line("%s", st)
		return from, from, "", errors.New("klasörde commit edilmemiş değişiklikler var; deploy bunların üzerine yazacağı için durduruldu")
	}
	if target == "" {
		l.line("▶ git: origin/%s çekiliyor", rn.p.Branch)
		cmd := rn.command(ctx, "git", "-c", "safe.directory="+rn.p.Dir, "fetch", "--prune", "origin", rn.p.Branch)
		if err := rn.stream(cmd); err != nil {
			return from, from, "", fmt.Errorf("git fetch başarısız (%v); depoya erişim (SSH anahtarı / token) gerekiyor olabilir", err)
		}
		cur, _ := rn.gitOut(ctx, "rev-parse", "--abbrev-ref", "HEAD")
		if cur != rn.p.Branch {
			if out, err := rn.gitOut(ctx, "checkout", "-q", rn.p.Branch); err != nil {
				return from, from, "", fmt.Errorf("%s dalına geçilemedi: %s", rn.p.Branch, out)
			}
		}
		if ahead, _ := rn.gitOut(ctx, "rev-list", "--count", "origin/"+rn.p.Branch+"..HEAD"); ahead != "" && ahead != "0" {
			return from, from, "", fmt.Errorf("sunucudaki %s dalında uzakta olmayan %s commit var; üzerine yazmamak için durduruldu", rn.p.Branch, ahead)
		}
		target = "origin/" + rn.p.Branch
	} else {
		l.line("▶ git: %s sürümüne geçiliyor", short(target))
		if _, err := rn.gitOut(ctx, "cat-file", "-e", target+"^{commit}"); err != nil {
			return from, from, "", errors.New("bu commit depoda bulunamadı")
		}
	}
	if out, err := rn.gitOut(ctx, "reset", "--hard", "-q", target); err != nil {
		return from, from, "", fmt.Errorf("git reset başarısız: %s", out)
	}
	to, _ = rn.gitOut(ctx, "rev-parse", "HEAD")
	msg, _ = rn.gitOut(ctx, "log", "-1", "--format=%s")
	if from == to {
		l.line("  %s · %s", short(to), msg)
	} else {
		l.line("  %s → %s · %s", short(from), short(to), msg)
		if lg, err := rn.gitOut(ctx, "log", "--oneline", "--no-decorate", "-15", from+".."+to); err == nil && lg != "" {
			for _, ln := range strings.Split(lg, "\n") {
				l.line("    %s", ln)
			}
		}
	}
	return from, to, msg, nil
}

func short(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

// Commits geri alma listesi için son commit'leri döner.
func (e *Engine) Commits(projectID string) ([]map[string]string, error) {
	p, ok := e.Project(projectID)
	if !ok {
		return nil, errors.New("proje bulunamadı")
	}
	if p.Branch == "" {
		return []map[string]string{}, nil
	}
	rn := &runner{p: p, l: &liveRun{notify: make(chan struct{})}}
	if err := rn.setup(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := rn.gitOut(ctx, "log", "-30", "--format=%H%x1f%s%x1f%an%x1f%ct", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("git log: %s", out)
	}
	head, _ := rn.gitOut(ctx, "rev-parse", "HEAD")
	list := []map[string]string{}
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Split(ln, "\x1f")
		if len(f) == 4 {
			list = append(list, map[string]string{"hash": f[0], "subject": f[1], "author": f[2], "time": f[3], "current": strconv.FormatBool(f[0] == head)})
		}
	}
	return list, nil
}

// Info proje klasörünün git durumunu döner (panelde gösterim için).
func (e *Engine) Info(p Project) map[string]string {
	if p.Branch == "" {
		return map[string]string{}
	}
	rn := &runner{p: p, l: &liveRun{notify: make(chan struct{})}}
	if rn.setup() != nil {
		return map[string]string{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := rn.gitOut(ctx, "log", "-1", "--format=%H%x1f%s%x1f%ct")
	f := strings.Split(out, "\x1f")
	if err != nil || len(f) != 3 {
		return map[string]string{}
	}
	remote, _ := rn.gitOut(ctx, "remote", "get-url", "origin")
	return map[string]string{"hash": f[0], "subject": f[1], "time": f[2], "remote": redact(remote)}
}

// redact uzak adresteki kullanıcı adı/token'ı gizler.
func redact(s string) string {
	if u, err := url.Parse(s); err == nil && u.User != nil {
		u.User = nil
		return u.String()
	}
	return s
}
