package deploy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func sh(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func wait(t *testing.T, e *Engine, id string) Run {
	t.Helper()
	var log strings.Builder
	e.Follow(context.Background(), id, func(b []byte) error { log.Write(b); return nil })
	for i := 0; i < 100; i++ {
		if r, _ := e.Run(id); r.Status != "running" {
			t.Logf("log:\n%s", log.String())
			return r
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("deploy bitmedi")
	return Run{}
}

func TestDeploy(t *testing.T) {
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin.git")
	work := filepath.Join(tmp, "work")
	app := filepath.Join(tmp, "app")
	sh(t, tmp, "git", "init", "-q", "--bare", "-b", "main", origin)
	sh(t, tmp, "git", "clone", "-q", origin, work)
	os.WriteFile(filepath.Join(work, "surum.txt"), []byte("1"), 0o644)
	sh(t, work, "git", "add", ".")
	sh(t, work, "git", "commit", "-q", "-m", "ilk sürüm")
	sh(t, work, "git", "push", "-q", "origin", "HEAD:main")
	sh(t, tmp, "git", "clone", "-q", "-b", "main", origin, app)
	c1 := sh(t, app, "git", "rev-parse", "HEAD")

	// yeni sürüm
	os.WriteFile(filepath.Join(work, "surum.txt"), []byte("2"), 0o644)
	sh(t, work, "git", "commit", "-qam", "ikinci sürüm")
	sh(t, work, "git", "push", "-q", "origin", "HEAD:main")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()

	e, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	restarted := 0
	e.Restart = func(string) error { restarted++; return nil }
	var fmu sync.Mutex
	var finished []string
	e.OnFinish = func(ev Event) { fmu.Lock(); finished = append(finished, ev.Run.Status); fmu.Unlock() }
	p, err := e.Save(Project{Name: "Uygulama", Dir: app, Branch: "main", PM2: "app",
		Steps: []Step{{Cmd: "cat surum.txt > derlendi.txt"}}, HealthURL: srv.URL, AutoRollback: true})
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.Start(p.ID, Options{Trigger: "panel", User: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Start(p.ID, Options{}); err != ErrBusy {
		t.Errorf("aynı anda iki deploy başladı: %v", err)
	}
	r = wait(t, e, r.ID)
	if r.Status != "ok" || r.From != c1 || r.Message != "ikinci sürüm" || restarted != 1 {
		t.Fatalf("deploy: %+v restart=%d", r, restarted)
	}
	if b, _ := os.ReadFile(filepath.Join(app, "derlendi.txt")); string(b) != "2" {
		t.Fatalf("adım çalışmadı: %q", b)
	}

	// hatalı sürüm → otomatik geri alma
	os.WriteFile(filepath.Join(work, "surum.txt"), []byte("3"), 0o644)
	sh(t, work, "git", "commit", "-qam", "bozuk sürüm")
	sh(t, work, "git", "push", "-q", "origin", "HEAD:main")
	p.Steps = []Step{{Name: "derle", Cmd: `test "$(cat surum.txt)" != 3 && cat surum.txt > derlendi.txt`}}
	p, _ = e.Save(p)
	r2, _ := e.Start(p.ID, Options{Trigger: "telegram"})
	r2 = wait(t, e, r2.ID)
	if r2.Status != "rolled_back" || !strings.Contains(r2.Error, "derle") {
		t.Fatalf("geri alma: %+v", r2)
	}
	if b, _ := os.ReadFile(filepath.Join(app, "surum.txt")); string(b) != "2" {
		t.Fatalf("geri alınmadı: %q", b)
	}

	// elle eski sürüme dönüş
	r3, _ := e.Start(p.ID, Options{Target: c1})
	r3 = wait(t, e, r3.ID)
	if r3.Status != "ok" || r3.To != c1 {
		t.Fatalf("elle geri alma: %+v", r3)
	}

	// commit edilmemiş değişiklik varsa durur
	os.WriteFile(filepath.Join(app, "surum.txt"), []byte("elle"), 0o644)
	r4, _ := e.Start(p.ID, Options{})
	r4 = wait(t, e, r4.ID)
	if r4.Status != "failed" || !strings.Contains(r4.Error, "commit edilmemiş") {
		t.Fatalf("kirli klasör: %+v", r4)
	}
	time.Sleep(200 * time.Millisecond)
	fmu.Lock()
	defer fmu.Unlock()
	if len(finished) != 4 || len(e.Runs(p.ID, 10)) != 4 {
		t.Fatalf("bildirimler: %v", finished)
	}
	if cs, err := e.Commits(p.ID); err != nil || len(cs) < 1 {
		t.Fatalf("commits: %v %v", cs, err)
	}
}

func TestCancel(t *testing.T) {
	e, _ := Open(t.TempDir())
	p, err := e.Save(Project{Name: "Uzun", Dir: t.TempDir(), Steps: []Step{{Cmd: "sleep 30"}}})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := e.Start(p.ID, Options{})
	time.Sleep(300 * time.Millisecond)
	if err := e.Cancel(r.ID); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	r = wait(t, e, r.ID)
	if r.Status != "canceled" || time.Since(start) > 5*time.Second {
		t.Fatalf("iptal: %+v", r)
	}
}

func TestValidate(t *testing.T) {
	d := t.TempDir()
	for _, p := range []Project{
		{Name: "x", Dir: "goreli", Steps: []Step{{Cmd: "ls"}}},
		{Name: "x", Dir: d, Branch: "main"},            // git deposu değil
		{Name: "x", Dir: d, Branch: "--upload-pack=x"}, // zararlı dal adı
		{Name: "x", Dir: d},                            // yapacak bir şey yok
		{Name: "x", Dir: d, PM2: "a; rm -rf /"},        // zararlı pm2 adı
		{Name: "x", Dir: d, PM2: "a", HealthURL: "ftp://x"},
	} {
		if Validate(&p) == nil {
			t.Errorf("kabul edilmemeliydi: %+v", p)
		}
	}
}
