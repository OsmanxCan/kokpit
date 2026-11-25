package files

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T) (Root, string) {
	t.Helper()
	tmp := t.TempDir()
	root := filepath.Join(tmp, "depo")
	outside := filepath.Join(tmp, "gizli")
	os.MkdirAll(filepath.Join(root, "a"), 0o755)
	os.MkdirAll(outside, 0o755)
	os.WriteFile(filepath.Join(outside, "sir.txt"), []byte("sır"), 0o600)
	os.WriteFile(filepath.Join(root, "a", "not.txt"), []byte("merhaba"), 0o644)
	os.Symlink(outside, filepath.Join(root, "kacis"))
	os.Symlink(filepath.Join(outside, "sir.txt"), filepath.Join(root, "kacis.txt"))
	os.Symlink(filepath.Join(root, "a"), filepath.Join(root, "ic"))
	return Root{Name: "Depo", Path: root, Write: true, Delete: true}, outside
}

func TestEscape(t *testing.T) {
	r, _ := setup(t)
	for _, p := range []string{"../gizli/sir.txt", "/../../etc/passwd", "kacis/sir.txt", "kacis/yeni.txt", "a/../../gizli"} {
		j, err := Resolve(r, p)
		if err == nil {
			if _, err2 := ReadText(j); err2 == nil {
				t.Errorf("%q kök dışına çıktı (%s)", p, j.Abs)
			}
		}
	}
	// "../" temizlenir ve kökte kalır
	if j, err := Resolve(r, "../../a/not.txt"); err != nil || j.Rel != "/a/not.txt" {
		t.Fatalf("temizleme: %v %+v", err, j)
	}
	// kök dışını gösteren bağlantı okunamaz ama silinebilir
	j, err := Resolve(r, "kacis.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadText(j); !errors.Is(err, ErrOutside) {
		t.Fatalf("bağlantı okundu: %v", err)
	}
	if _, err := WriteText(j, "x", 0, false); !errors.Is(err, ErrOutside) {
		t.Fatalf("bağlantı üzerinden yazıldı: %v", err)
	}
	if err := Delete(j); err != nil {
		t.Fatal(err)
	}
	// içeri gösteren bağlantı çalışır
	j, _ = Resolve(r, "ic/not.txt")
	if tf, err := ReadText(j); err != nil || tf.Content != "merhaba" {
		t.Fatalf("iç bağlantı: %v", err)
	}
	// liste
	jr, _ := Resolve(r, "/")
	l, err := List(jr)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range l.Entries {
		names = append(names, e.Name)
		if e.Name == "kacis" && !e.Outside {
			t.Error("kaçış bağlantısı işaretlenmedi")
		}
	}
	if strings.Join(names, ",") != "a,ic,kacis" {
		t.Errorf("liste: %v", names)
	}
	if Delete(jr) != ErrRoot {
		t.Error("kök silinebildi")
	}
}

func TestOps(t *testing.T) {
	r, _ := setup(t)
	d, _ := Resolve(r, "/a/b")
	if err := Mkdir(d); err != nil {
		t.Fatal(err)
	}
	f, _ := Resolve(r, "/a/b/yeni.txt")
	mt, err := WriteText(f, "ilk", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteText(f, "iki", mt-1, false); err == nil {
		t.Fatal("çakışma yakalanmadı")
	}
	if _, err := WriteText(f, "iki", mt, false); err != nil {
		t.Fatal(err)
	}
	to, _ := Resolve(r, "/a/b/ad.txt")
	if err := Move(f, to); err != nil {
		t.Fatal(err)
	}
	ab, _ := Resolve(r, "/a")
	into, _ := Resolve(r, "/a/b/a")
	if Move(ab, into) == nil {
		t.Fatal("klasör kendi içine taşındı")
	}
	// parçalı yükleme
	data := bytes.Repeat([]byte("0123456789"), 1000)
	w, done, err := Chunk(d, "buyuk.bin", "deneme-123", 0, int64(len(data)), false, bytes.NewReader(data[:4000]))
	if err != nil || done || w != 4000 {
		t.Fatalf("parça 1: %v %v %d", err, done, w)
	}
	if w, _, err := Chunk(d, "buyuk.bin", "deneme-123", 1000, int64(len(data)), false, bytes.NewReader(data)); err != ErrOffset || w != 4000 {
		t.Fatalf("yanlış offset: %v %d", err, w)
	}
	_, done, err = Chunk(d, "buyuk.bin", "deneme-123", 4000, int64(len(data)), false, bytes.NewReader(data[4000:]))
	if err != nil || !done {
		t.Fatalf("parça 2: %v %v", err, done)
	}
	got, _ := os.ReadFile(filepath.Join(d.Abs, "buyuk.bin"))
	if !bytes.Equal(got, data) {
		t.Fatal("yüklenen dosya bozuk")
	}
	if _, _, err := Chunk(d, "buyuk.bin", "deneme-456", 0, 3, false, strings.NewReader("abc")); err != ErrExists {
		t.Fatalf("üzerine yazma kontrolü: %v", err)
	}
	// zip
	var buf bytes.Buffer
	if err := Zip(&buf, []*Joined{ab}); err != nil {
		t.Fatal(err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	var zn []string
	for _, f := range zr.File {
		zn = append(zn, f.Name)
	}
	if !strings.Contains(strings.Join(zn, ","), "a/b/buyuk.bin") {
		t.Fatalf("zip: %v", zn)
	}
	// salt okunur
	ro := r
	ro.Write, ro.Delete = false, false
	x, _ := Resolve(ro, "/a/not.txt")
	if Delete(x) != ErrNoDelete {
		t.Fatal("silme izni olmadan silindi")
	}
	if _, err := WriteText(x, "y", 0, false); err != ErrReadOnly {
		t.Fatal("yazma izni olmadan yazıldı")
	}
	wo := r
	wo.Delete = false // yazabilir ama silemez
	y, _ := Resolve(wo, "/a/b/buyuk.bin")
	if Delete(y) != ErrNoDelete {
		t.Fatal("silme izni olmadan silindi")
	}
	if _, _, err := Chunk(d2(wo, t), "buyuk.bin", "deneme-789", 0, 3, true, strings.NewReader("abc")); err == nil {
		t.Fatal("silme izni olmadan üzerine yazıldı")
	}
}

func TestValidateRoot(t *testing.T) {
	for _, p := range []string{"/", "/etc", "/proc/1", "/dev/shm", "goreli/yol", "/var"} {
		r := Root{Name: "x", Path: p}
		if ValidateRoot(&r) == nil {
			t.Errorf("%s kabul edildi", p)
		}
	}
	for _, p := range []string{"/srv/depo", "/etc/nginx", "/var/www", "/root"} {
		r := Root{Name: "x", Path: p}
		if err := ValidateRoot(&r); err != nil {
			t.Errorf("%s reddedildi: %v", p, err)
		}
	}
}

func d2(r Root, t *testing.T) *Joined {
	j, err := Resolve(r, "/a/b")
	if err != nil {
		t.Fatal(err)
	}
	return j
}
