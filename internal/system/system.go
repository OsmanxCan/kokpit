// Package system sunucu metriklerini doğrudan /proc ve /sys'ten okur (dış bağımlılık yok).
package system

import (
	"bufio"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/OsmanxCan/kokpit/internal/store"
)

type Disk struct {
	Mount   string  `json:"mount"`
	Device  string  `json:"device"`
	FSType  string  `json:"fstype"`
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	Percent float64 `json:"percent"`
}

type Proc struct {
	PID     int     `json:"pid"`
	Name    string  `json:"name"`
	User    string  `json:"user"`
	CPU     float64 `json:"cpu"`
	MemRSS  uint64  `json:"mem"`
	MemPct  float64 `json:"memPct"`
	Cmdline string  `json:"cmd"`
}

type Temp struct {
	Name string  `json:"name"`
	C    float64 `json:"c"`
}

type Snapshot struct {
	TS        int64   `json:"ts"`
	CPU       float64 `json:"cpu"`
	CPUCores  int     `json:"cores"`
	CPUModel  string  `json:"cpuModel"`
	MemTotal  uint64  `json:"memTotal"`
	MemUsed   uint64  `json:"memUsed"`
	MemPct    float64 `json:"memPct"`
	SwapTotal uint64  `json:"swapTotal"`
	SwapUsed  uint64  `json:"swapUsed"`
	SwapPct   float64 `json:"swapPct"`
	Load1     float64 `json:"load1"`
	Load5     float64 `json:"load5"`
	Load15    float64 `json:"load15"`
	Uptime    uint64  `json:"uptime"`
	Hostname  string  `json:"hostname"`
	OS        string  `json:"os"`
	Kernel    string  `json:"kernel"`
	RXRate    float64 `json:"rx"` // byte/sn
	TXRate    float64 `json:"tx"`
	RXTotal   uint64  `json:"rxTotal"`
	TXTotal   uint64  `json:"txTotal"`
	Disks     []Disk  `json:"disks"`
	RootDisk  float64 `json:"rootDisk"`
	Temps     []Temp  `json:"temps"`
	Procs     []Proc  `json:"procs"`
	ProcCount int     `json:"procCount"`
}

const clkTck = 100 // Linux'ta USER_HZ neredeyse her zaman 100

type Collector struct {
	mu     sync.RWMutex
	latest Snapshot
	ring   []Snapshot
	st     *store.Store
	subs   map[chan Snapshot]struct{}
	minute []Snapshot
	static struct {
		host, os, kernel, model string
		cores                   int
	}
	lastCPU   [2]uint64 // idle, total
	lastNet   [2]uint64
	lastNetT  time.Time
	lastProc  map[int]uint64 // pid -> utime+stime
	lastProcT time.Time
	users     map[string]string
}

func New(st *store.Store) *Collector {
	c := &Collector{st: st, subs: map[chan Snapshot]struct{}{}, lastProc: map[int]uint64{}, users: map[string]string{}}
	c.static.host, _ = os.Hostname()
	c.static.os = osRelease()
	c.static.kernel = readTrim("/proc/sys/kernel/osrelease")
	c.static.model, c.static.cores = cpuInfo()
	return c
}

func readTrim(p string) string {
	b, _ := os.ReadFile(p)
	return strings.TrimSpace(string(b))
}

func osRelease() string {
	b, _ := os.ReadFile("/etc/os-release")
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(l, "PRETTY_NAME="), `"`)
		}
	}
	return "Linux"
}

func cpuInfo() (string, int) {
	b, _ := os.ReadFile("/proc/cpuinfo")
	model, n := "", 0
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "processor") {
			n++
		}
		if model == "" && strings.HasPrefix(l, "model name") {
			if i := strings.Index(l, ":"); i >= 0 {
				model = strings.TrimSpace(l[i+1:])
			}
		}
	}
	if n == 0 {
		n = 1
	}
	return model, n
}

func (c *Collector) Start() {
	c.cpuPercent()
	c.topProcs(0)
	go func() {
		time.Sleep(time.Second)
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			c.collect()
			<-t.C
		}
	}()
}

func (c *Collector) Latest() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latest
}

func (c *Collector) Ring() []Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Snapshot, len(c.ring))
	copy(out, c.ring)
	return out
}

func (c *Collector) Subscribe() (chan Snapshot, func()) {
	ch := make(chan Snapshot, 2)
	c.mu.Lock()
	c.subs[ch] = struct{}{}
	c.mu.Unlock()
	return ch, func() {
		c.mu.Lock()
		delete(c.subs, ch)
		c.mu.Unlock()
	}
}

func (c *Collector) cpuPercent() float64 {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	line := strings.SplitN(string(b), "\n", 2)[0]
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return 0
	}
	var total, idle uint64
	for i, v := range f[1:] {
		n, _ := strconv.ParseUint(v, 10, 64)
		if i >= 8 { // guest değerleri user'a zaten dahil
			break
		}
		total += n
		if i == 3 || i == 4 { // idle + iowait
			idle += n
		}
	}
	prevIdle, prevTotal := c.lastCPU[0], c.lastCPU[1]
	c.lastCPU = [2]uint64{idle, total}
	if prevTotal == 0 || total <= prevTotal {
		return 0
	}
	dt, di := float64(total-prevTotal), float64(idle-prevIdle)
	return (dt - di) / dt * 100
}

func meminfo() map[string]uint64 {
	m := map[string]uint64{}
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return m
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Fields(sc.Text())
		if len(p) >= 2 {
			n, _ := strconv.ParseUint(p[1], 10, 64)
			m[strings.TrimSuffix(p[0], ":")] = n * 1024
		}
	}
	return m
}

func skipIface(n string) bool {
	return n == "lo" || strings.HasPrefix(n, "veth") || strings.HasPrefix(n, "docker") || strings.HasPrefix(n, "br-")
}

func netTotals() (rx, tx uint64) {
	b, _ := os.ReadFile("/proc/net/dev")
	for _, l := range strings.Split(string(b), "\n") {
		i := strings.Index(l, ":")
		if i < 0 {
			continue
		}
		name := strings.TrimSpace(l[:i])
		if skipIface(name) {
			continue
		}
		f := strings.Fields(l[i+1:])
		if len(f) < 9 {
			continue
		}
		r, _ := strconv.ParseUint(f[0], 10, 64)
		t, _ := strconv.ParseUint(f[8], 10, 64)
		rx += r
		tx += t
	}
	return
}

var realFS = map[string]bool{"ext4": true, "ext3": true, "ext2": true, "xfs": true, "btrfs": true, "zfs": true, "f2fs": true, "vfat": false}

func disks() []Disk {
	b, _ := os.ReadFile("/proc/mounts")
	seen := map[string]bool{}
	var out []Disk
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) < 3 || !realFS[f[2]] || seen[f[0]] {
			continue
		}
		mount := strings.ReplaceAll(f[1], `\040`, " ")
		if strings.HasPrefix(mount, "/snap") || strings.HasPrefix(mount, "/var/lib/docker") {
			continue
		}
		var st syscall.Statfs_t
		if syscall.Statfs(mount, &st) != nil || st.Blocks == 0 {
			continue
		}
		seen[f[0]] = true
		bs := uint64(st.Bsize)
		used := (st.Blocks - st.Bfree) * bs
		total := used + st.Bavail*bs // df ile aynı hesap
		pct := 0.0
		if total > 0 {
			pct = float64(used) / float64(total) * 100
		}
		out = append(out, Disk{mount, f[0], f[2], st.Blocks * bs, used, pct})
	}
	return out
}

func temps() []Temp {
	var out []Temp
	zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
	for _, z := range zones {
		v, err := strconv.ParseFloat(readTrim(z+"/temp"), 64)
		if err == nil && v > 0 {
			out = append(out, Temp{readTrim(z + "/type"), v / 1000})
		}
	}
	if len(out) > 0 {
		return out
	}
	inputs, _ := filepath.Glob("/sys/class/hwmon/hwmon*/temp*_input")
	for _, in := range inputs {
		v, err := strconv.ParseFloat(readTrim(in), 64)
		if err != nil || v <= 0 {
			continue
		}
		dir := filepath.Dir(in)
		label := readTrim(strings.TrimSuffix(in, "_input") + "_label")
		if label == "" {
			label = readTrim(dir + "/name")
		}
		out = append(out, Temp{label, v / 1000})
	}
	return out
}

func (c *Collector) username(uid string) string {
	if n, ok := c.users[uid]; ok {
		return n
	}
	n := uid
	if u, err := user.LookupId(uid); err == nil {
		n = u.Username
	}
	c.users[uid] = n
	return n
}

func (c *Collector) topProcs(memTotal uint64) ([]Proc, int) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, 0
	}
	now := time.Now()
	dt := now.Sub(c.lastProcT).Seconds()
	first := c.lastProcT.IsZero()
	c.lastProcT = now
	page := uint64(os.Getpagesize())
	cur := map[int]uint64{}
	var list []Proc
	count := 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		count++
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		s := string(b)
		lp, rp := strings.Index(s, "("), strings.LastIndex(s, ")")
		if lp < 0 || rp < 0 || rp+2 > len(s) {
			continue
		}
		name := s[lp+1 : rp]
		f := strings.Fields(s[rp+2:])
		if len(f) < 22 {
			continue
		}
		ut, _ := strconv.ParseUint(f[11], 10, 64)
		stt, _ := strconv.ParseUint(f[12], 10, 64)
		rssPages, _ := strconv.ParseUint(f[21], 10, 64)
		ticks := ut + stt
		cur[pid] = ticks
		if first || memTotal == 0 {
			continue
		}
		cpu := 0.0
		if prev, ok := c.lastProc[pid]; ok && ticks >= prev && dt > 0 {
			cpu = float64(ticks-prev) / clkTck / dt * 100
		}
		rss := rssPages * page
		if cpu < 0.1 && rss < 40*1024*1024 {
			continue
		}
		uid := ""
		if st, err := os.ReadFile("/proc/" + e.Name() + "/status"); err == nil {
			for _, l := range strings.Split(string(st), "\n") {
				if strings.HasPrefix(l, "Uid:") {
					if ff := strings.Fields(l); len(ff) > 1 {
						uid = ff[1]
					}
					break
				}
			}
		}
		cmd := ""
		if cb, err := os.ReadFile("/proc/" + e.Name() + "/cmdline"); err == nil {
			cmd = strings.TrimSpace(strings.ReplaceAll(string(cb), "\x00", " "))
		}
		if len(cmd) > 220 {
			cmd = cmd[:220] + "…"
		}
		list = append(list, Proc{pid, name, c.username(uid), cpu, rss, float64(rss) / float64(memTotal) * 100, cmd})
	}
	c.lastProc = cur
	sort.Slice(list, func(i, j int) bool {
		if int(list[i].CPU*10) == int(list[j].CPU*10) {
			return list[i].MemRSS > list[j].MemRSS
		}
		return list[i].CPU > list[j].CPU
	})
	if len(list) > 20 {
		list = list[:20]
	}
	return list, count
}

func (c *Collector) collect() {
	s := Snapshot{TS: time.Now().Unix(), Hostname: c.static.host, OS: c.static.os, Kernel: c.static.kernel,
		CPUModel: c.static.model, CPUCores: c.static.cores}
	s.CPU = c.cpuPercent()

	m := meminfo()
	s.MemTotal = m["MemTotal"]
	if s.MemTotal > 0 {
		s.MemUsed = s.MemTotal - m["MemAvailable"]
		s.MemPct = float64(s.MemUsed) / float64(s.MemTotal) * 100
	}
	s.SwapTotal = m["SwapTotal"]
	if s.SwapTotal > 0 {
		s.SwapUsed = s.SwapTotal - m["SwapFree"]
		s.SwapPct = float64(s.SwapUsed) / float64(s.SwapTotal) * 100
	}

	if f := strings.Fields(readTrim("/proc/loadavg")); len(f) >= 3 {
		s.Load1, _ = strconv.ParseFloat(f[0], 64)
		s.Load5, _ = strconv.ParseFloat(f[1], 64)
		s.Load15, _ = strconv.ParseFloat(f[2], 64)
	}
	if f := strings.Fields(readTrim("/proc/uptime")); len(f) > 0 {
		u, _ := strconv.ParseFloat(f[0], 64)
		s.Uptime = uint64(u)
	}

	s.Disks = disks()
	for _, d := range s.Disks {
		if d.Mount == "/" {
			s.RootDisk = d.Percent
		}
	}

	rx, tx := netTotals()
	now := time.Now()
	if !c.lastNetT.IsZero() && rx >= c.lastNet[0] && tx >= c.lastNet[1] {
		dt := now.Sub(c.lastNetT).Seconds()
		s.RXRate = float64(rx-c.lastNet[0]) / dt
		s.TXRate = float64(tx-c.lastNet[1]) / dt
	}
	c.lastNet, c.lastNetT = [2]uint64{rx, tx}, now
	s.RXTotal, s.TXTotal = rx, tx

	s.Temps = temps()
	s.Procs, s.ProcCount = c.topProcs(s.MemTotal)

	c.mu.Lock()
	c.latest = s
	light := s
	light.Procs = nil
	c.ring = append(c.ring, light)
	if len(c.ring) > 720 { // son 1 saat
		c.ring = c.ring[len(c.ring)-720:]
	}
	c.minute = append(c.minute, light)
	subs := make([]chan Snapshot, 0, len(c.subs))
	for ch := range c.subs {
		subs = append(subs, ch)
	}
	var flush []Snapshot
	if len(c.minute) >= 12 {
		flush, c.minute = c.minute, nil
	}
	c.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- s:
		default:
		}
	}
	if flush != nil && c.st != nil {
		var p store.MetricPoint
		for _, f := range flush {
			p.CPU += f.CPU
			p.Mem += f.MemPct
			p.Swap += f.SwapPct
			p.Disk += f.RootDisk
			p.Load1 += f.Load1
			p.RX += f.RXRate
			p.TX += f.TXRate
		}
		n := float64(len(flush))
		p.TS = (s.TS / 60) * 60
		p.CPU, p.Mem, p.Swap, p.Disk, p.Load1, p.RX, p.TX = p.CPU/n, p.Mem/n, p.Swap/n, p.Disk/n, p.Load1/n, p.RX/n, p.TX/n
		c.st.AddMetric(p)
	}
}
