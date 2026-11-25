// Package geoip IPv4 adreslerinin hangi ülkeye ait olduğunu bulur.
//
// Veri: DB-IP "IP to Country Lite" (CC BY 4.0, https://db-ip.com). Veritabanı ayda bir
// sunucuya indirilir ve sorgular tamamen yerelde yapılır; IP adresleri hiçbir dış servise gönderilmez.
package geoip

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
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

type rng struct {
	start, end uint32
	cc         [2]byte
}

type DB struct {
	mu      sync.RWMutex
	path    string
	ranges  []rng
	updated time.Time
	source  string
	err     string
	loading bool
}

func Open(dir string) *DB { return &DB{path: filepath.Join(dir, "geoip-country.csv.gz")} }

// Status arayüz için.
type Status struct {
	Ready   bool   `json:"ready"`
	Loading bool   `json:"loading"`
	Updated int64  `json:"updated"`
	Ranges  int    `json:"ranges"`
	Source  string `json:"source"`
	Error   string `json:"error,omitempty"`
}

func (d *DB) Status() Status {
	d.mu.RLock()
	defer d.mu.RUnlock()
	s := Status{Ready: len(d.ranges) > 0, Loading: d.loading, Ranges: len(d.ranges), Source: d.source, Error: d.err}
	if !d.updated.IsZero() {
		s.Updated = d.updated.Unix()
	}
	return s
}

// Start dosya varsa yükler; yoksa veya 35 günden eskiyse indirir. Günde bir kontrol eder.
func (d *DB) Start() {
	go func() {
		for {
			fi, err := os.Stat(d.path)
			if err != nil || time.Since(fi.ModTime()) > 35*24*time.Hour {
				if err := d.download(); err != nil {
					d.setErr(err)
				}
			}
			d.mu.RLock()
			empty := len(d.ranges) == 0
			d.mu.RUnlock()
			if empty {
				if err := d.load(); err != nil && !os.IsNotExist(err) {
					d.setErr(err)
				}
			}
			time.Sleep(24 * time.Hour)
		}
	}()
}

func (d *DB) setErr(err error) {
	d.mu.Lock()
	d.err = err.Error()
	d.mu.Unlock()
}

func sources(now time.Time) []string {
	cur := now.Format("2006-01")
	prev := now.AddDate(0, -1, 0).Format("2006-01")
	return []string{
		"https://download.db-ip.com/free/dbip-country-lite-" + cur + ".csv.gz",
		"https://download.db-ip.com/free/dbip-country-lite-" + prev + ".csv.gz",
		// Aynı verinin npm paketi olarak yayınlanan kopyası (sadece IPv4)
		"https://cdn.jsdelivr.net/npm/@ip-location-db/dbip-country/dbip-country-ipv4.csv",
	}
}

// download veritabanını indirir, doğrular ve yükler.
func (d *DB) download() error {
	d.mu.Lock()
	d.loading = true
	d.mu.Unlock()
	defer func() { d.mu.Lock(); d.loading = false; d.mu.Unlock() }()

	cl := &http.Client{Timeout: 3 * time.Minute}
	var lastErr error
	for _, u := range sources(time.Now()) {
		req, _ := http.NewRequest("GET", u, nil)
		req.Header.Set("User-Agent", "Kokpit")
		resp, err := cl.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			lastErr = fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
			continue
		}
		tmp := d.path + ".tmp"
		f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			resp.Body.Close()
			return err
		}
		// düz CSV gelirse sıkıştırarak kaydet
		var w io.Writer = f
		var gz *gzip.Writer
		if strings.HasSuffix(u, ".csv") {
			gz = gzip.NewWriter(f)
			w = gz
		}
		_, err = io.Copy(w, io.LimitReader(resp.Body, 200<<20))
		resp.Body.Close()
		if gz != nil {
			gz.Close()
		}
		f.Close()
		if err != nil {
			os.Remove(tmp)
			lastErr = err
			continue
		}
		ranges, err := parseFile(tmp)
		if err != nil || len(ranges) < 50000 {
			os.Remove(tmp)
			lastErr = fmt.Errorf("indirilen dosya geçersiz: %v (%d kayıt)", err, len(ranges))
			continue
		}
		if err := os.Rename(tmp, d.path); err != nil {
			return err
		}
		d.mu.Lock()
		d.ranges, d.updated, d.source, d.err = ranges, time.Now(), u, ""
		d.mu.Unlock()
		return nil
	}
	return fmt.Errorf("ülke veritabanı indirilemedi: %v", lastErr)
}

func (d *DB) load() error {
	ranges, err := parseFile(d.path)
	if err != nil {
		return err
	}
	fi, _ := os.Stat(d.path)
	d.mu.Lock()
	d.ranges, d.updated, d.err = ranges, fi.ModTime(), ""
	if d.source == "" {
		d.source = "yerel dosya"
	}
	d.mu.Unlock()
	return nil
}

func parseFile(path string) ([]rng, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	return parse(gz)
}

// parse "başlangıç,bitiş,ülke" satırlarını okur; sadece IPv4 aralıklarını tutar.
func parse(r io.Reader) ([]rng, error) {
	out := make([]rng, 0, 350000)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 || bytes.IndexByte(line, ':') >= 0 { // IPv6 satırlarını atla
			continue
		}
		p := bytes.Split(line, []byte(","))
		if len(p) < 3 {
			continue
		}
		a, b := net.ParseIP(string(p[0])).To4(), net.ParseIP(string(p[1])).To4()
		cc := bytes.Trim(p[2], `" `)
		if a == nil || b == nil || len(cc) != 2 {
			continue
		}
		out = append(out, rng{binary.BigEndian.Uint32(a), binary.BigEndian.Uint32(b), [2]byte{cc[0], cc[1]}})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].start < out[j].start })
	return append(make([]rng, 0, len(out)), out...), nil // fazla kapasiteyi bırak
}

var ErrNotReady = errors.New("ülke veritabanı henüz hazır değil")

// Country IP'nin ülke kodunu (örn. "TR") döner; bulunamazsa "".
func (d *DB) Country(ipStr string) string {
	ip := net.ParseIP(ipStr).To4()
	if ip == nil {
		return ""
	}
	v := binary.BigEndian.Uint32(ip)
	d.mu.RLock()
	defer d.mu.RUnlock()
	i := sort.Search(len(d.ranges), func(i int) bool { return d.ranges[i].start > v }) - 1
	if i < 0 || v > d.ranges[i].end {
		return ""
	}
	cc := string(d.ranges[i].cc[:])
	if cc == "ZZ" {
		return ""
	}
	return cc
}
