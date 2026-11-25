package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/OsmanxCan/kokpit/internal/files"
	"github.com/OsmanxCan/kokpit/internal/settings"
)

// resolve istekteki kök adı ve yolu güvenle çözer.
func (s *Server) resolve(root, p string) (*files.Joined, error) {
	r, ok := s.set.Root(root)
	if !ok {
		return nil, errors.New("klasör bulunamadı: " + root)
	}
	return files.Resolve(r, p)
}

func fileErr(w http.ResponseWriter, err error) {
	code := 400
	switch {
	case errors.Is(err, files.ErrNotFound):
		code = 404
	case errors.Is(err, files.ErrOutside), errors.Is(err, files.ErrReadOnly), errors.Is(err, files.ErrNoDelete):
		code = 403
	case errors.Is(err, files.ErrExists):
		code = 409
	}
	writeErr(w, code, err.Error())
}

func (s *Server) handleFileRoots(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.set.Get().FileRoots)
}

func (s *Server) handleFileList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	j, err := s.resolve(q.Get("root"), q.Get("path"))
	if err != nil {
		fileErr(w, err)
		return
	}
	l, err := files.List(j)
	if err != nil {
		fileErr(w, err)
		return
	}
	writeJSON(w, l)
}

func (s *Server) handleFileRead(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	j, err := s.resolve(q.Get("root"), q.Get("path"))
	if err != nil {
		fileErr(w, err)
		return
	}
	t, err := files.ReadText(j)
	if err != nil {
		fileErr(w, err)
		return
	}
	writeJSON(w, t)
}

func (s *Server) handleFileWrite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root    string `json:"root"`
		Path    string `json:"path"`
		Content string `json:"content"`
		MTime   int64  `json:"mtime,string"`
		Create  bool   `json:"create"`
	}
	// readJSON 256 KB ile sınırlı; düzenleyici 2 MB'a kadar dosya gönderebilir
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, files.MaxEdit*2+4096)).Decode(&req); err != nil {
		writeErr(w, 400, "Geçersiz istek: "+err.Error())
		return
	}
	j, err := s.resolve(req.Root, req.Path)
	if err == nil {
		var mt int64
		mt, err = files.WriteText(j, req.Content, req.MTime, req.Create)
		if err == nil {
			s.fileAudit(r, map[bool]string{true: "dosya oluştur", false: "dosya düzenle"}[req.Create], req.Root, j.Rel, nil)
			writeJSON(w, map[string]any{"ok": true, "mtime": strconv.FormatInt(mt, 10)})
			return
		}
	}
	s.fileAudit(r, "dosya kaydet", req.Root, req.Path, err)
	fileErr(w, err)
}

func (s *Server) handleFileMkdir(w http.ResponseWriter, r *http.Request) {
	var req struct{ Root, Path string }
	if !readJSON(w, r, &req) {
		return
	}
	j, err := s.resolve(req.Root, req.Path)
	if err == nil {
		err = files.Mkdir(j)
	}
	s.fileAudit(r, "klasör oluştur", req.Root, req.Path, err)
	if err != nil {
		fileErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleFileMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root string   `json:"root"`
		From []string `json:"from"`
		To   string   `json:"to"`   // hedef klasör (taşıma) ...
		Name string   `json:"name"` // ... veya yeni ad (tek öğe, aynı klasörde)
	}
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.From) == 0 || len(req.From) > 500 {
		writeErr(w, 400, "taşınacak öğe yok")
		return
	}
	var fails []string
	for _, f := range req.From {
		target := path.Join(req.To, path.Base(f))
		if req.Name != "" {
			target = path.Join(path.Dir(path.Clean("/"+f)), req.Name)
		}
		from, err := s.resolve(req.Root, f)
		var to *files.Joined
		if err == nil {
			to, err = s.resolve(req.Root, target)
		}
		if err == nil {
			err = files.Move(from, to)
		}
		act := "taşı"
		if req.Name != "" {
			act = "ad değiştir"
		}
		s.fileAudit(r, act, req.Root, f+" → "+target, err)
		if err != nil {
			fails = append(fails, path.Base(f)+": "+err.Error())
		}
	}
	if len(fails) > 0 {
		writeErr(w, 400, strings.Join(fails, "\n"))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleFileDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root  string   `json:"root"`
		Paths []string `json:"paths"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	var fails []string
	for _, p := range req.Paths {
		j, err := s.resolve(req.Root, p)
		if err == nil {
			err = files.Delete(j)
		}
		s.fileAudit(r, "sil", req.Root, p, err)
		if err != nil {
			fails = append(fails, path.Base(p)+": "+err.Error())
		}
	}
	if len(fails) > 0 {
		writeErr(w, 400, strings.Join(fails, "\n"))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleFileUpload bir parçayı alır. Sorgu: root, path (klasör), name, id, offset, total, overwrite.
func (s *Server) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	offset, err1 := strconv.ParseInt(q.Get("offset"), 10, 64)
	total, err2 := strconv.ParseInt(q.Get("total"), 10, 64)
	if err1 != nil || err2 != nil || offset < 0 || total < 0 || offset > total {
		writeErr(w, 400, "geçersiz parça bilgisi")
		return
	}
	dir, err := s.resolve(q.Get("root"), q.Get("path"))
	if err != nil {
		fileErr(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 101<<20) // parça en fazla ~100 MB
	written, done, err := files.Chunk(dir, q.Get("name"), q.Get("id"), offset, total, q.Get("overwrite") == "1", r.Body)
	if errors.Is(err, files.ErrOffset) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(409)
		fmt.Fprintf(w, `{"error":"offset","received":%d}`, written)
		return
	}
	if err != nil {
		if offset == 0 || done {
			s.fileAudit(r, "yükle", q.Get("root"), path.Join(dir.Rel, q.Get("name")), err)
		}
		fileErr(w, err)
		return
	}
	if done {
		s.fileAudit(r, "yükle", q.Get("root"), path.Join(dir.Rel, q.Get("name"))+" ("+humanBytes(total)+")", nil)
	}
	writeJSON(w, map[string]any{"received": written, "done": done})
}

func (s *Server) handleFileUploadCancel(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if dir, err := s.resolve(q.Get("root"), q.Get("path")); err == nil {
		files.CancelUpload(dir, q.Get("id"))
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleFileDownload dosyayı indirir (inline=1 ile güvenli türleri önizler).
func (s *Server) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	j, err := s.resolve(q.Get("root"), q.Get("path"))
	if err == nil {
		inline := q.Get("inline") == "1"
		if err = files.Serve(w, r, j, inline); err == nil {
			if !inline && r.Header.Get("Range") == "" {
				s.fileAudit(r, "indir", q.Get("root"), j.Rel, nil)
			}
			return
		}
	}
	fileErr(w, err)
}

// handleFileZip birden çok öğeyi veya bir klasörü zip olarak indirir.
func (s *Server) handleFileZip(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	root := q.Get("root")
	var items []*files.Joined
	for _, p := range q["path"] {
		j, err := s.resolve(root, p)
		if err != nil {
			fileErr(w, err)
			return
		}
		items = append(items, j)
	}
	if len(items) == 0 {
		writeErr(w, 400, "indirilecek öğe yok")
		return
	}
	name := path.Base(items[0].Rel)
	if len(items) > 1 || name == "/" {
		name = root + "-" + time.Now().Format("20060102-1504")
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename*=UTF-8''`+urlEscape(name+".zip"))
	w.Header().Set("Cache-Control", "private, no-store")
	var rels []string
	for _, j := range items {
		rels = append(rels, j.Rel)
	}
	s.fileAudit(r, "zip indir", root, strings.Join(rels, ", "), nil)
	files.Zip(w, items)
}

func (s *Server) fileAudit(r *http.Request, action, root, target string, err error) {
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "dosya: "+action, root+":"+target, err == nil, errStr(err))
}

func humanBytes(n int64) string {
	const u = "KMGT"
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	f, i := float64(n)/1024, 0
	for f >= 1024 && i < len(u)-1 {
		f /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %cB", f, u[i])
}

func urlEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-_.~", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// ---------- erişim izinleri ----------

// handleFileBrowse klasör seçici: sadece alt klasör adlarını döner.
func (s *Server) handleFileBrowse(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		p = "/"
	}
	b, err := files.BrowseDirs(p)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	granted := map[string]string{}
	for _, fr := range s.set.Get().FileRoots {
		granted[fr.Path] = fr.Name
	}
	writeJSON(w, map[string]any{"browse": b, "granted": granted})
}

type grantReq struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Write  bool   `json:"write"`
	Delete bool   `json:"delete"`
}

func permText(fr files.Root) string {
	t := "okuma"
	if fr.Write {
		t += "+yazma"
	}
	if fr.Delete {
		t += "+silme"
	}
	return t
}

func (s *Server) handleFileGrant(w http.ResponseWriter, r *http.Request) {
	var req grantReq
	if !readJSON(w, r, &req) {
		return
	}
	fr := files.Root{Name: req.Name, Path: req.Path, Write: req.Write, Delete: req.Delete}
	if strings.TrimSpace(fr.Name) == "" {
		fr.Name = path.Base(path.Clean("/" + strings.TrimSpace(req.Path)))
	}
	err := files.ValidateRoot(&fr)
	if err == nil {
		if fi, e := os.Stat(fr.Path); e != nil || !fi.IsDir() {
			err = errors.New(fr.Path + " bulunamadı veya bir klasör değil")
		}
	}
	if err == nil {
		err = s.set.Update(func(c *settings.Settings) {
			for _, x := range c.FileRoots {
				if x.Name == fr.Name {
					err = errors.New("bu adda bir klasör zaten var: " + fr.Name)
					return
				}
				if x.Path == fr.Path {
					err = errors.New("bu klasöre zaten erişim verilmiş (" + x.Name + ")")
					return
				}
			}
			if len(c.FileRoots) >= 20 {
				err = errors.New("en fazla 20 klasöre erişim verilebilir")
				return
			}
			c.FileRoots = append(c.FileRoots, fr)
		})
	}
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "dosya: erişim ver", fr.Path+" ("+permText(fr)+")", err == nil, errStr(err))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, s.set.Get().FileRoots)
}

func (s *Server) handleFileRootUpdate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req grantReq
	if !readJSON(w, r, &req) {
		return
	}
	var found *files.Root
	var err error
	newName := strings.TrimSpace(req.Name)
	err2 := s.set.Update(func(c *settings.Settings) {
		for i := range c.FileRoots {
			if newName != "" && newName != name && c.FileRoots[i].Name == newName {
				err = errors.New("bu adda bir klasör zaten var: " + newName)
				return
			}
		}
		for i := range c.FileRoots {
			x := &c.FileRoots[i]
			if x.Name == name {
				if newName != "" {
					tmp := files.Root{Name: newName, Path: x.Path}
					if e := files.ValidateRoot(&tmp); e != nil {
						err = e
						return
					}
					x.Name = tmp.Name
				}
				x.Write, x.Delete = req.Write, req.Delete
				cp := *x
				found = &cp
				return
			}
		}
	})
	if err == nil {
		err = err2
	}
	if err == nil && found == nil {
		err = errors.New("klasör bulunamadı: " + name)
	}
	si := sess(r)
	target := name
	if found != nil {
		target = found.Path + " (" + permText(*found) + ")"
	}
	s.st.Audit(si.Username, clientIP(r), "dosya: izin değiştir", target, err == nil, errStr(err))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, s.set.Get().FileRoots)
}

func (s *Server) handleFileRevoke(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var removed string
	err := s.set.Update(func(c *settings.Settings) {
		for i, x := range c.FileRoots {
			if x.Name == name {
				removed = x.Path
				c.FileRoots = append(c.FileRoots[:i], c.FileRoots[i+1:]...)
				return
			}
		}
	})
	if err == nil && removed == "" {
		err = errors.New("klasör bulunamadı: " + name)
	}
	si := sess(r)
	s.st.Audit(si.Username, clientIP(r), "dosya: erişimi kaldır", removed, err == nil, errStr(err))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, s.set.Get().FileRoots)
}
