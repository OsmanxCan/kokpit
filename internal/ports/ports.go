// Package ports sunucuda dinleyen portları listeler ve hangilerinin dışarıdan
// erişilebilir olduğunu firewall (UFW) kurallarıyla karşılaştırarak bulur.
package ports

import (
	"errors"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OsmanxCan/kokpit/internal/firewall"
)

type Item struct {
	Proto    string   `json:"proto"` // tcp | udp
	Port     int      `json:"port"`
	Addrs    []string `json:"addrs"` // dinlenen adresler
	Program  string   `json:"program"`
	PID      int      `json:"pid"`
	Exposure string   `json:"exposure"` // open | limited | protected | local
	Reason   string   `json:"reason"`
	Docker   bool     `json:"docker"`
	Risk     string   `json:"risk,omitempty"` // dışarı açık olmaması gereken bilinen servis
	Since    int64    `json:"since"`          // ilk görülme
}

func (i Item) Key() string { return i.Proto + "/" + strconv.Itoa(i.Port) }

// Dışarıya açık olması genelde risk olan servisler.
var risky = map[int]string{
	3306: "MySQL", 5432: "PostgreSQL", 6379: "Redis", 27017: "MongoDB", 9200: "Elasticsearch", 11211: "Memcached",
	2375: "Docker API (şifresiz)", 2376: "Docker API", 5984: "CouchDB", 8086: "InfluxDB", 9000: "MinIO / Portainer",
	5900: "VNC", 3389: "RDP", 6443: "Kubernetes API", 2379: "etcd", 15672: "RabbitMQ yönetim", 5672: "RabbitMQ",
	9090: "Prometheus", 3000: "geliştirme sunucusu", 8080: "geliştirme / yönetim paneli",
}

var userRe = regexp.MustCompile(`\(\("([^"]+)",pid=(\d+)`)

// ParseSS `ss -H -tulpn` çıktısını okur.
func ParseSS(out string) []Item {
	byKey := map[string]*Item{}
	var order []string
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 5 {
			continue
		}
		proto := f[0]
		if proto != "tcp" && proto != "udp" {
			continue
		}
		if proto == "tcp" && f[1] != "LISTEN" {
			continue
		}
		local := f[4]
		i := strings.LastIndexByte(local, ':')
		if i < 0 {
			continue
		}
		host, portS := local[:i], local[i+1:]
		port, err := strconv.Atoi(portS)
		if err != nil || port == 0 {
			continue
		}
		host = strings.Trim(host, "[]")
		if j := strings.IndexByte(host, '%'); j >= 0 {
			host = host[:j]
		}
		it := byKey[proto+"/"+portS]
		if it == nil {
			it = &Item{Proto: proto, Port: port}
			byKey[proto+"/"+portS] = it
			order = append(order, proto+"/"+portS)
		}
		if !contains(it.Addrs, host) {
			it.Addrs = append(it.Addrs, host)
		}
		if m := userRe.FindStringSubmatch(ln); m != nil && it.Program == "" {
			it.Program = m[1]
			it.PID, _ = strconv.Atoi(m[2])
		}
		if it.Program == "docker-proxy" {
			it.Docker = true
		}
	}
	out2 := make([]Item, 0, len(order))
	for _, k := range order {
		out2 = append(out2, *byKey[k])
	}
	sort.Slice(out2, func(a, b int) bool {
		if out2[a].Port != out2[b].Port {
			return out2[a].Port < out2[b].Port
		}
		return out2[a].Proto < out2[b].Proto
	})
	return out2
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func loopback(h string) bool {
	return h == "localhost" || strings.HasPrefix(h, "127.") || h == "::1" || strings.HasPrefix(h, "::ffff:127.")
}

// ---------- UFW eşleştirme ----------

// portSpec UFW kuralının hedef kısmı: "22/tcp", "80,443/tcp", "8000:8100", "OpenSSH"...
type portSpec struct {
	ranges [][2]int
	proto  string // "" = ikisi de
	any    bool   // tüm portlar ("Anywhere")
}

func (p portSpec) match(proto string, port int) bool {
	if p.proto != "" && p.proto != proto {
		return false
	}
	if p.any {
		return true
	}
	for _, r := range p.ranges {
		if port >= r[0] && port <= r[1] {
			return true
		}
	}
	return false
}

var appPorts = map[string]string{
	"OpenSSH": "22/tcp", "Nginx Full": "80,443/tcp", "Nginx HTTP": "80/tcp", "Nginx HTTPS": "443/tcp",
	"Apache": "80/tcp", "Apache Full": "80,443/tcp", "Apache Secure": "443/tcp", "WWW": "80/tcp", "WWW Full": "80,443/tcp",
	"WWW Secure": "443/tcp", "Postfix": "25/tcp", "Dovecot IMAP": "143/tcp", "Dovecot Secure IMAP": "993/tcp",
}

var (
	appMu    sync.Mutex
	appCache = map[string]string{}
)

// appInfo bilinmeyen UFW uygulama profilinin portlarını `ufw app info` ile bulur.
func appInfo(name string) string {
	if p, ok := appPorts[name]; ok {
		return p
	}
	appMu.Lock()
	defer appMu.Unlock()
	if p, ok := appCache[name]; ok {
		return p
	}
	out, _ := exec.Command("ufw", "app", "info", name).CombinedOutput()
	var specs []string
	lines := strings.Split(string(out), "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "Port") {
			for _, x := range lines[i+1:] {
				if x = strings.TrimSpace(x); x != "" {
					specs = append(specs, x)
				}
			}
		}
	}
	p := strings.Join(specs, ";")
	appCache[name] = p
	return p
}

func parseSpec(to string) []portSpec {
	to = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(to), "(v6)"))
	if i := strings.Index(to, " on "); i >= 0 {
		to = to[:i]
	}
	if to == "Anywhere" || to == "" {
		return []portSpec{{any: true}}
	}
	if !strings.ContainsAny(to[:1], "0123456789") {
		to = appInfo(to)
		if to == "" {
			return nil
		}
	}
	var out []portSpec
	for _, part := range strings.Split(to, ";") {
		ports, proto, _ := strings.Cut(strings.TrimSpace(part), "/")
		ps := portSpec{proto: proto}
		for _, r := range strings.Split(ports, ",") {
			a, b, isRange := strings.Cut(r, ":")
			x, err1 := strconv.Atoi(a)
			y := x
			var err2 error
			if isRange {
				y, err2 = strconv.Atoi(b)
			}
			if err1 == nil && err2 == nil {
				ps.ranges = append(ps.ranges, [2]int{x, y})
			}
		}
		if len(ps.ranges) > 0 {
			out = append(out, ps)
		}
	}
	return out
}

// Classify her port için dışarıdan erişilebilirliği belirler.
func Classify(items []Item, fw firewall.Status) []Item {
	for i := range items {
		it := &items[i]
		allLocal := true
		for _, a := range it.Addrs {
			if !loopback(a) {
				allLocal = false
			}
		}
		switch {
		case allLocal:
			it.Exposure, it.Reason = "local", "sadece sunucunun içinden erişilebilir"
		case it.Docker:
			it.Exposure, it.Reason = "open", "Docker portu: Docker kendi kurallarını yazdığı için UFW bunu engelleyemez"
		case !fw.Installed || !fw.Active:
			it.Exposure, it.Reason = "open", "firewall kapalı"
		case fw.Incoming == "allow":
			it.Exposure, it.Reason = "open", "firewall gelen bağlantılara varsayılan olarak izin veriyor"
		default:
			it.Exposure, it.Reason = "protected", "firewall engelliyor"
			for _, r := range fw.Rules {
				if !strings.HasPrefix(r.Action, "ALLOW") && !strings.HasPrefix(r.Action, "LIMIT") {
					continue
				}
				if strings.Contains(r.Action, "OUT") || strings.Contains(r.Action, "FWD") {
					continue
				}
				for _, sp := range parseSpec(r.To) {
					if !sp.match(it.Proto, it.Port) {
						continue
					}
					if strings.HasPrefix(r.From, "Anywhere") {
						it.Exposure = "open"
						it.Reason = "firewall'da herkese izin verilmiş"
						if strings.HasPrefix(r.Action, "LIMIT") {
							it.Reason = "firewall'da herkese izin verilmiş (hız sınırlı)"
						}
					} else if it.Exposure != "open" {
						it.Exposure, it.Reason = "limited", "sadece "+r.From+" adresinden"
					}
				}
			}
		}
		if name, ok := risky[it.Port]; ok {
			switch it.Exposure {
			case "open":
				it.Risk = name
			case "protected", "limited":
				it.Reason += "; ama tüm adreslerde dinliyor, sadece 127.0.0.1'de dinlemesi daha güvenli"
			}
		}
	}
	return items
}

// ---------- izleme ----------

type Watcher struct {
	mu       sync.Mutex
	items    []Item
	first    map[string]int64 // dışarı açık portun ilk görülme zamanı
	seen     map[string]int64 // son 24 saatte dışarı açık görülenler
	base     bool
	err      string
	at       int64
	OnNew    func(Item)
	Firewall func() firewall.Status
}

func NewWatcher() *Watcher {
	return &Watcher{first: map[string]int64{}, seen: map[string]int64{}, Firewall: firewall.Get}
}

func run() (string, error) {
	if _, err := exec.LookPath("ss"); err != nil {
		return "", errors.New("ss komutu bulunamadı (iproute2 paketi)")
	}
	out, err := exec.Command("ss", "-H", "-tulpn").Output()
	return string(out), err
}

// Refresh portları yeniden okur; dışarıya yeni açılan port varsa OnNew çağrılır.
func (w *Watcher) Refresh() {
	out, err := run()
	now := time.Now().Unix()
	if err != nil {
		w.mu.Lock()
		w.err, w.at = err.Error(), now
		w.mu.Unlock()
		return
	}
	w.apply(Classify(ParseSS(out), w.Firewall()))
}

func (w *Watcher) apply(items []Item) {
	now := time.Now().Unix()
	var fresh []Item
	w.mu.Lock()
	for i := range items {
		it := &items[i]
		if it.Exposure != "open" {
			continue
		}
		k := it.Key()
		if _, ok := w.first[k]; !ok {
			w.first[k] = now
		}
		it.Since = w.first[k]
		if w.base && now-w.seen[k] > 24*3600 {
			fresh = append(fresh, *it)
		}
		w.seen[k] = now
	}
	w.items, w.err, w.at, w.base = items, "", now, true
	cb := w.OnNew
	w.mu.Unlock()
	if cb != nil {
		for _, it := range fresh {
			cb(it)
		}
	}
}

// Start dakikada bir kontrol eder.
func (w *Watcher) Start() {
	go func() {
		for {
			w.Refresh()
			time.Sleep(time.Minute)
		}
	}()
}

type Snapshot struct {
	Items []Item `json:"items"`
	Error string `json:"error,omitempty"`
	At    int64  `json:"at"`
}

func (w *Watcher) Snapshot() Snapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	return Snapshot{Items: append([]Item{}, w.items...), Error: w.err, At: w.at}
}
