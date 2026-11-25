package geoip

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Gerçek veriyle test: /tmp/ipdb/package/dbip-country-ipv4.csv varsa kullanılır.
func TestRealDB(t *testing.T) {
	src := "/tmp/ipdb/package/dbip-country-ipv4.csv"
	in, err := os.Open(src)
	if err != nil {
		t.Skip("gerçek veri yok")
	}
	defer in.Close()
	dir := t.TempDir()
	d := Open(dir)
	out, _ := os.Create(filepath.Join(dir, "geoip-country.csv.gz"))
	gz := gzip.NewWriter(out)
	io.Copy(gz, in)
	gz.Close()
	out.Close()
	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	if err := d.load(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.ReadMemStats(&m1)
	t.Logf("%d aralık, bellek ~%d MB", d.Status().Ranges, (m1.HeapInuse-m0.HeapInuse)>>20)
	for ip, want := range map[string]string{"8.8.8.8": "US", "1.0.1.5": "CN", "88.255.1.1": "TR", "10.0.0.1": "", "x": ""} {
		if got := d.Country(ip); got != want {
			t.Errorf("%s → %q, beklenen %q", ip, got, want)
		}
	}
}

func TestParse(t *testing.T) {
	r, err := parse(stringsReader("1.0.0.0,1.0.0.255,AU\n2001:200::,2001:200:ffff::,JP\n5.0.0.0,5.0.0.255,TR\n"))
	if err != nil || len(r) != 2 {
		t.Fatalf("%v %d", err, len(r))
	}
}

type sr struct {
	s string
	i int
}

func (r *sr) Read(p []byte) (int, error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}
	n := copy(p, r.s[r.i:])
	r.i += n
	return n, nil
}

func stringsReader(s string) io.Reader { return &sr{s: s} }
