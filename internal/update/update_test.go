package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.2.10", "1.2.9", true}, {"v1.0.0", "0.9.9", true}, {"1.0.0", "1.0.0", false},
		{"0.1.0", "dev", true}, {"1.0.0-beta", "0.9.0", true}, {"0.1.0", "0.2.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q,%q)=%v", c.a, c.b, got)
		}
	}
}

func TestApply(t *testing.T) {
	bin := []byte("#!/bin/sh\necho yeni\n")
	sum := sha256.Sum256(bin)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/a/b/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v9.0.0","assets":[{"name":%q,"browser_download_url":"%s/bin"},{"name":"checksums.txt","browser_download_url":"%s/sums"}]}`,
				assetName(), srv.URL, srv.URL)
		case "/bin":
			w.Write(bin)
		case "/sums":
			fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), assetName())
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	exe := filepath.Join(dir, "kokpit")
	os.WriteFile(exe, []byte("eski"), 0o755)
	c := New("1.0.0", "a/b", true)
	c.APIBase, c.ExePath = srv.URL, exe
	v, err := c.Apply()
	if err != nil || v != "9.0.0" {
		t.Fatalf("%v %v", v, err)
	}
	got, _ := os.ReadFile(exe)
	old, _ := os.ReadFile(exe + ".eski")
	if string(got) != string(bin) || string(old) != "eski" {
		t.Fatal("binary değişmedi")
	}
	// bozuk özet reddedilmeli
	sum[0] ^= 0xff
	c2 := New("1.0.0", "a/b", true)
	c2.APIBase, c2.ExePath = srv.URL, exe
	if _, err := c2.Apply(); err == nil {
		t.Fatal("bozuk özet kabul edildi")
	}
}
