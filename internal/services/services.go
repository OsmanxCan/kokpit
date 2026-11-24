// Package services PM2 süreçlerini, Docker container'larını ve systemd servislerini yönetir.
package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/OsmanxCan/kokpit/internal/config"
	"github.com/OsmanxCan/kokpit/internal/execx"
)

type Item struct {
	Kind    string  `json:"kind"` // pm2 | docker | systemd
	Name    string  `json:"name"`
	ID      string  `json:"id"`
	Status  string  `json:"status"` // online, stopped, errored, running, exited, active, failed...
	Healthy bool    `json:"healthy"`
	CPU     float64 `json:"cpu"`
	Mem     uint64  `json:"mem"`
	Uptime  int64   `json:"uptime"` // başlama zamanı (unix ms) veya 0
	Restart int     `json:"restarts"`
	User    string  `json:"user"`
	Info    string  `json:"info"` // ek bilgi: image, port, açıklama, cwd
	Ports   string  `json:"ports"`
}

type Manager struct {
	cfg    *config.Config
	pm2Bin string
}

func New(cfg *config.Config) *Manager {
	m := &Manager{cfg: cfg, pm2Bin: cfg.PM2Bin}
	if m.pm2Bin == "" {
		m.pm2Bin = findPM2()
	}
	return m
}

// findPM2 nvm ile kurulmuş pm2'yi de bulur (sistem servisi PATH'inde olmayabilir).
func findPM2() string {
	if p, err := exec.LookPath("pm2"); err == nil {
		return p
	}
	cands, _ := filepath.Glob("/root/.nvm/versions/node/*/bin/pm2")
	home, _ := os.UserHomeDir()
	more, _ := filepath.Glob(filepath.Join(home, ".nvm/versions/node/*/bin/pm2"))
	cands = append(cands, more...)
	cands = append(cands, "/usr/local/bin/pm2", "/usr/bin/pm2")
	sort.Sort(sort.Reverse(sort.StringSlice(cands)))
	for _, c := range cands {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func (m *Manager) pm2Cmd(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, m.pm2Bin, args...)
	env := os.Environ()
	// pm2 bir node betiği; nvm'in node'unu PATH'e ekle
	env = append(env, "PATH="+filepath.Dir(m.pm2Bin)+":"+os.Getenv("PATH"))
	if m.cfg.PM2Home != "" {
		env = append(env, "PM2_HOME="+m.cfg.PM2Home)
	}
	cmd.Env = env
	return cmd
}

func (m *Manager) pm2Run(args ...string) (string, error) {
	if m.pm2Bin == "" {
		return "", errors.New("pm2 bulunamadı")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := m.pm2Cmd(ctx, args...).CombinedOutput()
	return string(out), err
}

type pm2Proc struct {
	Name  string `json:"name"`
	PMID  int    `json:"pm_id"`
	Monit struct {
		Memory uint64  `json:"memory"`
		CPU    float64 `json:"cpu"`
	} `json:"monit"`
	Env struct {
		Status   string `json:"status"`
		PMUptime int64  `json:"pm_uptime"`
		Restarts int    `json:"restart_time"`
		Username string `json:"username"`
		Cwd      string `json:"pm_cwd"`
		OutLog   string `json:"pm_out_log_path"`
		ErrLog   string `json:"pm_err_log_path"`
		ExecPath string `json:"pm_exec_path"`
		Unstable int    `json:"unstable_restarts"`
		Version  string `json:"version"`
	} `json:"pm2_env"`
}

func (m *Manager) pm2List() ([]pm2Proc, error) {
	out, err := m.pm2Run("jlist")
	if err != nil {
		return nil, fmt.Errorf("pm2 jlist: %v", err)
	}
	// pm2 bazen JSON'dan önce uyarı basar; ilk '[' karakterinden başla
	if i := strings.Index(out, "["); i > 0 {
		out = out[i:]
	}
	var list []pm2Proc
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (m *Manager) PM2() ([]Item, error) {
	if m.pm2Bin == "" {
		return []Item{}, nil
	}
	list, err := m.pm2List()
	if err != nil {
		return nil, err
	}
	items := []Item{}
	for _, p := range list {
		items = append(items, Item{
			Kind: "pm2", Name: p.Name, ID: fmt.Sprint(p.PMID), Status: p.Env.Status,
			Healthy: p.Env.Status == "online", CPU: p.Monit.CPU, Mem: p.Monit.Memory,
			Uptime: p.Env.PMUptime, Restart: p.Env.Restarts, User: p.Env.Username, Info: p.Env.Cwd,
		})
	}
	return items, nil
}

// PM2LogFiles bir PM2 sürecinin log dosyalarını döner.
func (m *Manager) PM2LogFiles(name string) ([]string, error) {
	list, err := m.pm2List()
	if err != nil {
		return nil, err
	}
	for _, p := range list {
		if p.Name == name || fmt.Sprint(p.PMID) == name {
			return []string{p.Env.OutLog, p.Env.ErrLog}, nil
		}
	}
	return nil, errors.New("süreç bulunamadı")
}

type dockerPS struct {
	ID     string `json:"ID"`
	Names  string `json:"Names"`
	Image  string `json:"Image"`
	State  string `json:"State"`
	Status string `json:"Status"`
	Ports  string `json:"Ports"`
}

func (m *Manager) Docker() ([]Item, error) {
	if !execx.Exists("docker") {
		return []Item{}, nil
	}
	out, err := execx.Run("docker", "ps", "-a", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, fmt.Errorf("docker ps: %v %s", err, out)
	}
	stats := map[string][2]string{}
	if so, err := execx.RunTimeout(15*time.Second, "docker", "stats", "--no-stream", "--format", "{{.Name}}|{{.CPUPerc}}|{{.MemUsage}}"); err == nil {
		for _, l := range strings.Split(strings.TrimSpace(so), "\n") {
			p := strings.Split(l, "|")
			if len(p) == 3 {
				stats[p[0]] = [2]string{p[1], p[2]}
			}
		}
	}
	items := []Item{}
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l == "" {
			continue
		}
		var d dockerPS
		if json.Unmarshal([]byte(l), &d) != nil {
			continue
		}
		it := Item{Kind: "docker", Name: d.Names, ID: shortID(d.ID), Status: d.State, Healthy: d.State == "running",
			Info: d.Image + " · " + d.Status, Ports: d.Ports}
		if s, ok := stats[d.Names]; ok {
			fmt.Sscanf(strings.TrimSuffix(s[0], "%"), "%f", &it.CPU)
			it.Mem = parseDockerMem(s[1])
		}
		items = append(items, it)
	}
	return items, nil
}

func shortID(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func parseDockerMem(s string) uint64 {
	// "12.3MiB / 1.9GiB"
	f := strings.TrimSpace(strings.Split(s, "/")[0])
	var v float64
	var unit string
	fmt.Sscanf(f, "%f%s", &v, &unit)
	mult := map[string]float64{"B": 1, "KiB": 1024, "MiB": 1 << 20, "GiB": 1 << 30, "kB": 1000, "MB": 1e6, "GB": 1e9}
	if m, ok := mult[unit]; ok {
		return uint64(v * m)
	}
	return 0
}

type sdUnit struct {
	Unit        string `json:"unit"`
	Load        string `json:"load"`
	Active      string `json:"active"`
	Sub         string `json:"sub"`
	Description string `json:"description"`
}

// Systemd servis listesi. all=false ise sadece çalışan, hatalı ve izlenen servisler.
func (m *Manager) Systemd(all bool) ([]Item, error) {
	if !execx.Exists("systemctl") {
		return []Item{}, nil
	}
	out, err := execx.Run("systemctl", "list-units", "--type=service", "--all", "--no-pager", "--output=json")
	if err != nil {
		return nil, fmt.Errorf("systemctl: %v", err)
	}
	var units []sdUnit
	if err := json.Unmarshal([]byte(out), &units); err != nil {
		return nil, err
	}
	watch := map[string]bool{}
	for _, w := range m.cfg.WatchServices {
		watch[strings.TrimSuffix(w, ".service")] = true
	}
	items := []Item{}
	for _, u := range units {
		name := strings.TrimSuffix(u.Unit, ".service")
		if u.Load == "not-found" {
			continue
		}
		interesting := u.Active == "active" && u.Sub == "running" || u.Active == "failed" || watch[name]
		if !all && !interesting {
			continue
		}
		items = append(items, Item{Kind: "systemd", Name: name, ID: u.Unit, Status: u.Active + "/" + u.Sub,
			Healthy: u.Active == "active", Info: u.Description, User: map[bool]string{true: "izleniyor"}[watch[name]]})
	}
	return items, nil
}

// Action başlat/durdur/yeniden başlat işlemini uygular.
func (m *Manager) Action(kind, name, action string) (string, error) {
	if !execx.SafeName(name) {
		return "", errors.New("geçersiz ad")
	}
	switch kind {
	case "pm2":
		valid := map[string]bool{"start": true, "stop": true, "restart": true, "reload": true}
		if !valid[action] {
			return "", errors.New("geçersiz işlem")
		}
		out, err := m.pm2Run(action, name)
		if err == nil {
			m.pm2Run("save")
		}
		return out, err
	case "docker":
		valid := map[string]bool{"start": true, "stop": true, "restart": true}
		if !valid[action] {
			return "", errors.New("geçersiz işlem")
		}
		return execx.RunTimeout(60*time.Second, "docker", action, name)
	case "systemd":
		valid := map[string]bool{"start": true, "stop": true, "restart": true, "reload": true, "enable": true, "disable": true}
		if !valid[action] {
			return "", errors.New("geçersiz işlem")
		}
		return execx.RunTimeout(60*time.Second, "systemctl", action, name)
	}
	return "", errors.New("bilinmeyen tür")
}

// LogCommand canlı log için çalıştırılacak komutu hazırlar.
func (m *Manager) LogCommand(ctx context.Context, kind, name string, lines int) (*exec.Cmd, error) {
	if !execx.SafeName(name) {
		return nil, errors.New("geçersiz ad")
	}
	n := fmt.Sprint(lines)
	switch kind {
	case "pm2":
		files, err := m.PM2LogFiles(name)
		if err != nil {
			return nil, err
		}
		args := []string{"-F", "-n", n}
		for _, f := range files {
			if f != "" && f != "/dev/null" {
				args = append(args, f)
			}
		}
		return exec.CommandContext(ctx, "tail", args...), nil
	case "docker":
		return exec.CommandContext(ctx, "docker", "logs", "-f", "--tail", n, "--timestamps", name), nil
	case "systemd":
		return exec.CommandContext(ctx, "journalctl", "-u", name, "-f", "-n", n, "--no-pager", "-o", "short-iso"), nil
	}
	return nil, errors.New("bilinmeyen tür")
}
