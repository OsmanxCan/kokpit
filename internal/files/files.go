// Package files panelde tanımlı klasörler (kökler) içinde güvenli dosya işlemleri yapar.
//
// Her yol bir köke göre çözülür; ".." ile veya kökün dışına işaret eden sembolik
// bağlantılarla kökten çıkılamaz. Kök dışındaki hiçbir dosyaya erişilemez.
package files

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// Root panelden erişim izni verilmiş bir klasör. Okuma her zaman vardır;
// yazma (yükleme, oluşturma, düzenleme, taşıma) ve silme ayrı ayrı verilir.
type Root struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Write    bool   `json:"write"`
	Delete   bool   `json:"delete"`
	ReadOnly bool   `json:"readOnly,omitempty"` // eski sürümlerden kalan alan (geçişte kullanılır)
}

var (
	ErrOutside  = errors.New("bu konum izin verilen klasörün dışında")
	ErrReadOnly = errors.New("bu klasöre yazma izni yok")
	ErrNoDelete = errors.New("bu klasörde silme izni yok")
	ErrNotFound = errors.New("dosya veya klasör bulunamadı")
	ErrExists   = errors.New("bu adda bir dosya veya klasör zaten var")
	ErrBadName  = errors.New("geçersiz ad")
	ErrRoot     = errors.New("kök klasörün kendisi üzerinde bu işlem yapılamaz")
)

// Yasak kökler: sistemin kendisi. Alt klasörlerine izin verilir (örn. /etc/nginx),
// sanal dosya sistemlerinin hiçbir yerine izin verilmez.
var forbiddenExact = map[string]bool{"/": true, "/etc": true, "/usr": true, "/bin": true, "/sbin": true, "/lib": true,
	"/lib64": true, "/boot": true, "/run": true, "/var": true}
var forbiddenTree = []string{"/proc", "/sys", "/dev"}

// ValidateRoot panelden eklenen kökü doğrular ve yolu temizler.
func ValidateRoot(r *Root) error {
	r.Name = strings.TrimSpace(r.Name)
	r.Path = filepath.Clean(strings.TrimSpace(r.Path))
	if r.Name == "" || utf8.RuneCountInString(r.Name) > 40 || strings.ContainsAny(r.Name, "/\\") {
		return errors.New("klasör adı 1–40 karakter olmalı ve / içermemeli")
	}
	if !filepath.IsAbs(r.Path) {
		return errors.New("yol / ile başlayan tam bir yol olmalı (örn. /srv/depo)")
	}
	if forbiddenExact[r.Path] {
		return fmt.Errorf("%s sistemin tamamını kapsadığı için eklenemez; içindeki belirli bir klasörü ekle", r.Path)
	}
	for _, f := range forbiddenTree {
		if r.Path == f || strings.HasPrefix(r.Path, f+"/") {
			return fmt.Errorf("%s sanal bir sistem klasörü, eklenemez", r.Path)
		}
	}
	return nil
}

// Joined bir kök ile kökün içindeki göreli yol.
type Joined struct {
	Root Root
	Base string // kökün gerçek (sembolik bağlantısı çözülmüş) yolu
	Rel  string // "/" ile başlayan, temizlenmiş göreli yol
	Abs  string // tam yol
}

// Resolve göreli yolu kök içinde güvenle çözer. Var olan en derin ata dizinin
// sembolik bağlantıları çözülür ve sonucun kök içinde kaldığı doğrulanır.
func Resolve(root Root, rel string) (*Joined, error) {
	if strings.ContainsRune(rel, 0) {
		return nil, ErrBadName
	}
	base, err := filepath.EvalSymlinks(root.Path)
	if err != nil {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil, fmt.Errorf("%s klasörü sunucuda yok. Ayarlar → Dosyalar ve terminal bölümünden yolu düzelt veya klasörü oluştur", root.Path)
		case errors.Is(err, fs.ErrPermission):
			return nil, fmt.Errorf("%s klasörüne erişim izni yok (panel root olarak çalışmıyor olabilir)", root.Path)
		}
		return nil, fmt.Errorf("%s açılamadı: %v", root.Path, friendly(err))
	}
	clean := path.Clean("/" + strings.ReplaceAll(rel, "\\", "/"))
	abs := filepath.Join(base, filepath.FromSlash(clean))
	if abs == base {
		return &Joined{Root: root, Base: base, Rel: "/", Abs: abs}, nil
	}
	// Son öğe hariç var olan en derin atayı bul ve sembolik bağlantılarını çöz.
	// Son öğe bir bağlantıysa takip etmesi gereken işlemler checkLeaf ile ayrıca doğrular;
	// böylece kök dışını gösteren bir bağlantının kendisi silinebilir/taşınabilir.
	probe, rest := filepath.Dir(abs), filepath.Base(abs)
	for {
		real, err := filepath.EvalSymlinks(probe)
		if err == nil {
			if !within(base, filepath.Join(real, rest)) {
				return nil, ErrOutside
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return nil, ErrOutside
		}
		rest = filepath.Join(filepath.Base(probe), rest)
		probe = parent
	}
	return &Joined{Root: root, Base: base, Rel: clean, Abs: abs}, nil
}

func within(base, p string) bool {
	return p == base || strings.HasPrefix(p, base+string(filepath.Separator))
}

// checkLeaf yolun son öğesi sembolik bağlantıysa hedefinin de kök içinde olduğunu doğrular.
func (j *Joined) checkLeaf() error {
	real, err := filepath.EvalSymlinks(j.Abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	if !within(j.Base, real) {
		return ErrOutside
	}
	return nil
}

func (j *Joined) writable() error {
	if !j.Root.Write {
		return ErrReadOnly
	}
	return nil
}

func (j *Joined) deletable() error {
	if !j.Root.Delete {
		return ErrNoDelete
	}
	return nil
}

func validName(n string) error {
	if n == "" || n == "." || n == ".." || strings.ContainsAny(n, "/\\\x00") || len(n) > 255 {
		return ErrBadName
	}
	return nil
}

// ---------- listeleme ----------

type Entry struct {
	Name    string `json:"name"`
	Dir     bool   `json:"dir"`
	Size    int64  `json:"size"`
	MTime   int64  `json:"mtime"`
	Mode    string `json:"mode"`
	Link    bool   `json:"link"`
	Outside bool   `json:"outside,omitempty"` // kök dışını gösteren bağlantı (açılamaz)
	Owner   uint32 `json:"uid"`
}

type Listing struct {
	Path    string  `json:"path"`
	Entries []Entry `json:"entries"`
	Write   bool    `json:"write"`
	Delete  bool    `json:"delete"`
	Free    uint64  `json:"free"`
	Total   uint64  `json:"total"`
}

const tmpPrefix = ".kokpit-yukleniyor-"

func List(j *Joined) (*Listing, error) {
	if err := j.checkLeaf(); err != nil {
		return nil, err
	}
	des, err := os.ReadDir(j.Abs)
	if err != nil {
		return nil, friendly(err)
	}
	l := &Listing{Path: j.Rel, Entries: make([]Entry, 0, len(des)), Write: j.Root.Write, Delete: j.Root.Delete}
	var sf syscall.Statfs_t
	if syscall.Statfs(j.Abs, &sf) == nil {
		l.Free = sf.Bavail * uint64(sf.Bsize)
		l.Total = sf.Blocks * uint64(sf.Bsize)
	}
	for _, de := range des {
		name := de.Name()
		info, err := de.Info()
		if err != nil {
			continue
		}
		if strings.HasPrefix(name, tmpPrefix) {
			// yarım kalmış yüklemeler: 1 günden eskiyse temizle, listede gösterme
			if time.Since(info.ModTime()) > 24*time.Hour && j.Root.Write {
				os.Remove(filepath.Join(j.Abs, name))
			}
			continue
		}
		e := Entry{Name: name, Size: info.Size(), MTime: info.ModTime().Unix(), Mode: info.Mode().Perm().String()}
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			e.Owner = st.Uid
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			e.Link = true
			full := filepath.Join(j.Abs, name)
			real, err := filepath.EvalSymlinks(full)
			if err != nil || !within(j.Base, real) {
				e.Outside = true
			} else if ti, err := os.Stat(real); err == nil {
				e.Dir, e.Size = ti.IsDir(), ti.Size()
			}
		} else {
			e.Dir = info.IsDir()
		}
		if e.Dir {
			e.Size = 0
		}
		l.Entries = append(l.Entries, e)
	}
	sort.Slice(l.Entries, func(a, b int) bool {
		x, y := l.Entries[a], l.Entries[b]
		if x.Dir != y.Dir {
			return x.Dir
		}
		return strings.ToLower(x.Name) < strings.ToLower(y.Name)
	})
	return l, nil
}

// ---------- okuma / yazma ----------

const MaxEdit = 2 << 20 // düzenleyicide açılabilecek en büyük dosya

type TextFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	MTime   int64  `json:"mtime,string"` // nano saniye (JS sayısına sığmadığı için metin); kaydederken çakışma kontrolü için
	Size    int64  `json:"size"`
	Binary  bool   `json:"binary"`
	TooBig  bool   `json:"tooBig"`
}

func ReadText(j *Joined) (*TextFile, error) {
	if err := j.checkLeaf(); err != nil {
		return nil, err
	}
	fi, err := os.Stat(j.Abs)
	if err != nil {
		return nil, friendly(err)
	}
	if fi.IsDir() {
		return nil, errors.New("bu bir klasör")
	}
	t := &TextFile{Path: j.Rel, MTime: fi.ModTime().UnixNano(), Size: fi.Size()}
	if fi.Size() > MaxEdit {
		t.TooBig = true
		return t, nil
	}
	b, err := os.ReadFile(j.Abs)
	if err != nil {
		return nil, friendly(err)
	}
	if !utf8.Valid(b) || strings.ContainsRune(string(b[:min(len(b), 8000)]), 0) {
		t.Binary = true
		return t, nil
	}
	t.Content = string(b)
	return t, nil
}

// WriteText dosyayı atomik olarak yazar. expectMTime > 0 ise dosya bu arada
// değiştirildiyse yazmaz. İzinler ve sahiplik korunur.
func WriteText(j *Joined, content string, expectMTime int64, create bool) (int64, error) {
	if err := j.writable(); err != nil {
		return 0, err
	}
	if len(content) > MaxEdit {
		return 0, errors.New("dosya en fazla 2 MB olabilir")
	}
	mode := fs.FileMode(0o644)
	uid, gid := -1, -1
	fi, err := os.Stat(j.Abs)
	switch {
	case err == nil:
		if create {
			return 0, ErrExists
		}
		if err := j.checkLeaf(); err != nil {
			return 0, err
		}
		if fi.IsDir() {
			return 0, errors.New("bu bir klasör")
		}
		if expectMTime > 0 && fi.ModTime().UnixNano() != expectMTime {
			return 0, errors.New("dosya sen düzenlerken başka biri tarafından değiştirildi; yeniden aç")
		}
		mode = fi.Mode().Perm()
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			uid, gid = int(st.Uid), int(st.Gid)
		}
	case errors.Is(err, fs.ErrNotExist):
		if !create {
			return 0, ErrNotFound
		}
		if err := validName(filepath.Base(j.Abs)); err != nil || j.Rel == "/" {
			return 0, ErrBadName
		}
	default:
		return 0, friendly(err)
	}
	// sembolik bağlantıyı değil hedef dosyayı güncelle
	target := j.Abs
	if real, err := filepath.EvalSymlinks(j.Abs); err == nil {
		target = real
	}
	tmp := filepath.Join(filepath.Dir(target), tmpPrefix+randHex())
	if err := os.WriteFile(tmp, []byte(content), mode); err != nil {
		return 0, friendly(err)
	}
	os.Chmod(tmp, mode)
	if uid >= 0 {
		os.Lchown(tmp, uid, gid)
	} else {
		inheritOwner(tmp, filepath.Dir(target))
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return 0, friendly(err)
	}
	nfi, _ := os.Stat(target)
	return nfi.ModTime().UnixNano(), nil
}

// inheritOwner yeni dosyanın sahibini bulunduğu klasörün sahibi yapar
// (panel root çalıştığı için aksi halde dosyalar root'a ait olur).
func inheritOwner(p, dir string) {
	if fi, err := os.Stat(dir); err == nil {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			os.Lchown(p, int(st.Uid), int(st.Gid))
		}
	}
}

// ---------- klasör, ad değiştirme, silme ----------

func Mkdir(j *Joined) error {
	if err := j.writable(); err != nil {
		return err
	}
	if err := validName(filepath.Base(j.Abs)); err != nil || j.Rel == "/" {
		return ErrBadName
	}
	if err := os.Mkdir(j.Abs, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return ErrExists
		}
		return friendly(err)
	}
	inheritOwner(j.Abs, filepath.Dir(j.Abs))
	return nil
}

// Move from'u to'ya taşır veya adını değiştirir (aynı kök içinde).
func Move(from, to *Joined) error {
	if err := from.writable(); err != nil {
		return err
	}
	if from.Rel == "/" || to.Rel == "/" {
		return ErrRoot
	}
	if err := validName(filepath.Base(to.Abs)); err != nil {
		return err
	}
	if _, err := os.Lstat(from.Abs); err != nil {
		return ErrNotFound
	}
	if _, err := os.Lstat(to.Abs); err == nil {
		return ErrExists
	}
	if strings.HasPrefix(to.Rel+"/", from.Rel+"/") {
		return errors.New("bir klasör kendi içine taşınamaz")
	}
	if err := os.Rename(from.Abs, to.Abs); err != nil {
		return friendly(err)
	}
	return nil
}

// Delete dosyayı veya klasörü (içindekilerle birlikte) siler. Sembolik bağlantılar
// takip edilmez; bağlantının kendisi silinir.
func Delete(j *Joined) error {
	if err := j.deletable(); err != nil {
		return err
	}
	if j.Rel == "/" {
		return ErrRoot
	}
	if _, err := os.Lstat(j.Abs); err != nil {
		return ErrNotFound
	}
	return friendly(os.RemoveAll(j.Abs))
}

// ---------- yükleme (parçalı) ----------

// Chunk büyük dosyaları parça parça yükler (Cloudflare tek istekte 100 MB'tan büyük
// gövdeye izin vermez). Parçalar gizli bir geçici dosyaya eklenir; son parçada dosya
// gerçek adına taşınır. offset geçici dosyanın boyutuyla eşleşmezse gerçek boyut
// döner ki istemci kaldığı yerden devam edebilsin.
func Chunk(dir *Joined, name, id string, offset, total int64, overwrite bool, body io.Reader) (written int64, done bool, err error) {
	if err := dir.writable(); err != nil {
		return 0, false, err
	}
	if err := validName(name); err != nil {
		return 0, false, err
	}
	if !idOK(id) {
		return 0, false, errors.New("geçersiz yükleme kimliği")
	}
	if err := dir.checkLeaf(); err != nil {
		return 0, false, err
	}
	final := filepath.Join(dir.Abs, name)
	tmp := filepath.Join(dir.Abs, tmpPrefix+id)
	if offset == 0 {
		if fi, err := os.Lstat(final); err == nil {
			if !overwrite {
				return 0, false, ErrExists
			}
			if err := dir.deletable(); err != nil {
				return 0, false, errors.New("aynı adda dosya var; üzerine yazmak için silme izni gerekir")
			}
			if fi.IsDir() {
				return 0, false, errors.New("aynı adda bir klasör var")
			}
		}
		var sf syscall.Statfs_t
		if syscall.Statfs(dir.Abs, &sf) == nil && uint64(total) > sf.Bavail*uint64(sf.Bsize) {
			return 0, false, errors.New("diskte yeterli boş alan yok")
		}
	}
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, false, friendly(err)
	}
	defer f.Close()
	fi, _ := f.Stat()
	if fi.Size() != offset {
		return fi.Size(), false, ErrOffset
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return 0, false, err
	}
	n, err := io.Copy(f, io.LimitReader(body, total-offset))
	written = offset + n
	if err != nil {
		return written, false, fmt.Errorf("yükleme kesildi: %v", err)
	}
	if written < total {
		return written, false, nil
	}
	f.Close()
	if fi, err := os.Lstat(final); err == nil {
		if !overwrite || fi.IsDir() {
			os.Remove(tmp)
			return written, false, ErrExists
		}
	}
	inheritOwner(tmp, dir.Abs)
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return written, false, friendly(err)
	}
	return written, true, nil
}

var ErrOffset = errors.New("parça sırası uyuşmuyor")

// CancelUpload yarım kalan yüklemenin geçici dosyasını siler.
func CancelUpload(dir *Joined, id string) {
	if idOK(id) && dir.Root.Write {
		os.Remove(filepath.Join(dir.Abs, tmpPrefix+id))
	}
}

func idOK(id string) bool {
	if len(id) < 8 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// ---------- indirme ----------

// inlineTypes tarayıcıda gösterilmesi güvenli türler (önizleme için).
// HTML, SVG gibi betik çalıştırabilen türler her zaman indirilir.
var inlineTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp",
	".avif": "image/avif", ".bmp": "image/bmp", ".ico": "image/x-icon",
	".mp4": "video/mp4", ".webm": "video/webm", ".mp3": "audio/mpeg", ".ogg": "audio/ogg", ".wav": "audio/wav",
	".txt": "text/plain; charset=utf-8",
}

// Serve dosyayı gönderir. inline=true ise güvenli türler tarayıcıda açılır.
func Serve(w http.ResponseWriter, r *http.Request, j *Joined, inline bool) error {
	if err := j.checkLeaf(); err != nil {
		return err
	}
	f, err := os.Open(j.Abs)
	if err != nil {
		return friendly(err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return errors.New("bu bir klasör")
	}
	name := filepath.Base(j.Abs)
	ct, safe := inlineTypes[strings.ToLower(filepath.Ext(name))]
	h := w.Header()
	if inline && safe {
		h.Set("Content-Type", ct)
		h.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
	} else {
		h.Set("Content-Type", "application/octet-stream")
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	}
	h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'; sandbox")
	h.Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, name, fi.ModTime(), f)
	return nil
}

// Zip klasörü (veya birden çok öğeyi) akış halinde zip olarak gönderir.
// Kök dışına işaret eden sembolik bağlantılar atlanır.
func Zip(w io.Writer, items []*Joined) error {
	zw := zip.NewWriter(w)
	for _, j := range items {
		if err := j.checkLeaf(); err != nil {
			continue
		}
		parent := filepath.Dir(j.Abs)
		filepath.WalkDir(j.Abs, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if d.Type()&fs.ModeSymlink != 0 {
				real, err := filepath.EvalSymlinks(p)
				if err != nil || !within(j.Base, real) {
					return nil
				}
				fi, err := os.Stat(real)
				if err != nil || fi.IsDir() { // bağlantılı klasörlere girilmez
					return nil
				}
				info = fi
			}
			rel, _ := filepath.Rel(parent, p)
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), tmpPrefix) {
					return fs.SkipDir
				}
				_, err := zw.CreateHeader(&zip.FileHeader{Name: rel + "/", Modified: info.ModTime(), Method: zip.Store})
				return err
			}
			if strings.HasPrefix(d.Name(), tmpPrefix) {
				return nil
			}
			hdr, _ := zip.FileInfoHeader(info)
			hdr.Name = rel
			hdr.Method = zip.Deflate
			if compressed(d.Name()) {
				hdr.Method = zip.Store
			}
			fw, err := zw.CreateHeader(hdr)
			if err != nil {
				return err
			}
			f, err := os.Open(p)
			if err != nil {
				return nil
			}
			_, err = io.Copy(fw, f)
			f.Close()
			return err
		})
	}
	return zw.Close()
}

func compressed(n string) bool {
	switch strings.ToLower(filepath.Ext(n)) {
	case ".zip", ".gz", ".tgz", ".xz", ".bz2", ".7z", ".rar", ".zst", ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif",
		".mp4", ".mkv", ".webm", ".mov", ".mp3", ".ogg", ".flac", ".pdf", ".docx", ".xlsx", ".pptx", ".apk", ".jar":
		return true
	}
	return false
}

// ---------- yardımcılar ----------

func friendly(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrNotExist):
		return ErrNotFound
	case errors.Is(err, fs.ErrPermission):
		return errors.New("izin reddedildi")
	case errors.Is(err, syscall.ENOSPC):
		return errors.New("diskte yer kalmadı")
	case errors.Is(err, syscall.EXDEV):
		return errors.New("farklı diskler arasında taşınamaz")
	case errors.Is(err, syscall.ENOTEMPTY):
		return errors.New("klasör boş değil")
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Err
	}
	return err
}

func randHex() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------- klasör seçici ----------

// DirInfo izin verilecek klasörü seçerken gösterilen alt klasör. Sadece adlar
// döner; dosyalar ve içerik izin verilmeden gösterilmez.
type DirInfo struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Link      bool   `json:"link,omitempty"`
	Forbidden string `json:"forbidden,omitempty"` // izin verilemiyorsa nedeni
	Hidden    bool   `json:"hidden,omitempty"`
}

type Browse struct {
	Path      string    `json:"path"`
	Parent    string    `json:"parent"`
	Dirs      []DirInfo `json:"dirs"`
	Files     int       `json:"files"`     // içerideki dosya sayısı (adları gösterilmez)
	Forbidden string    `json:"forbidden"` // bu klasörün kendisine izin verilemiyorsa nedeni
	Truncated bool      `json:"truncated"`
}

// forbiddenReason klasöre erişim izni verilip verilemeyeceğini söyler.
func forbiddenReason(p string) string {
	r := Root{Name: "x", Path: p}
	if err := ValidateRoot(&r); err != nil {
		return err.Error()
	}
	return ""
}

// BrowseDirs sunucudaki bir klasörün alt klasörlerini listeler (klasör seçici için).
func BrowseDirs(p string) (*Browse, error) {
	p = filepath.Clean("/" + strings.TrimSpace(p))
	for _, f := range forbiddenTree {
		if p == f || strings.HasPrefix(p, f+"/") {
			return nil, fmt.Errorf("%s sanal bir sistem klasörü, içine girilemez", f)
		}
	}
	des, err := os.ReadDir(p)
	if err != nil {
		return nil, friendly(err)
	}
	b := &Browse{Path: p, Parent: filepath.Dir(p), Dirs: []DirInfo{}, Forbidden: forbiddenReason(p)}
	if p == "/" {
		b.Parent = ""
	}
	for _, de := range des {
		full := filepath.Join(p, de.Name())
		isDir := de.IsDir()
		link := de.Type()&fs.ModeSymlink != 0
		if link {
			if fi, err := os.Stat(full); err == nil && fi.IsDir() {
				isDir = true
			}
		}
		if !isDir {
			b.Files++
			continue
		}
		if len(b.Dirs) >= 2000 {
			b.Truncated = true
			continue
		}
		d := DirInfo{Name: de.Name(), Path: full, Link: link, Hidden: strings.HasPrefix(de.Name(), ".")}
		for _, f := range forbiddenTree {
			if full == f {
				d.Forbidden = "sanal sistem klasörü"
			}
		}
		b.Dirs = append(b.Dirs, d)
	}
	sort.Slice(b.Dirs, func(i, j int) bool {
		if b.Dirs[i].Hidden != b.Dirs[j].Hidden {
			return !b.Dirs[i].Hidden
		}
		return strings.ToLower(b.Dirs[i].Name) < strings.ToLower(b.Dirs[j].Name)
	})
	return b, nil
}
