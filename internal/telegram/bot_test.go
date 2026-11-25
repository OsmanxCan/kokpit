package telegram

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OsmanxCan/kokpit/internal/config"
	"github.com/OsmanxCan/kokpit/internal/deploy"
	"github.com/OsmanxCan/kokpit/internal/services"
	"github.com/OsmanxCan/kokpit/internal/settings"
	"github.com/OsmanxCan/kokpit/internal/store"
	"github.com/OsmanxCan/kokpit/internal/system"
	"github.com/OsmanxCan/kokpit/internal/uptime"
)

// sahte Telegram API: gelen güncellemeleri sırayla verir, gönderilen mesajları kaydeder.
type fake struct {
	mu     sync.Mutex
	queue  [][]map[string]any
	sent   []map[string]any
	nextID int64
}

func (f *fake) push(u ...map[string]any) {
	f.mu.Lock()
	for _, x := range u {
		f.nextID++
		x["update_id"] = f.nextID
	}
	f.queue = append(f.queue, u)
	f.mu.Unlock()
}

func (f *fake) texts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, s := range f.sent {
		if t, ok := s["text"].(string); ok {
			out = append(out, t)
		}
	}
	return out
}

func (f *fake) lastKeyboardData() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.sent) - 1; i >= 0; i-- {
		if rm, ok := f.sent[i]["reply_markup"].(map[string]any); ok {
			var out []string
			for _, row := range rm["inline_keyboard"].([]any) {
				for _, b := range row.([]any) {
					out = append(out, b.(map[string]any)["callback_data"].(string))
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return nil
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	body, _ := io.ReadAll(r.Body)
	var p map[string]any
	json.Unmarshal(body, &p)
	var result any = true
	switch method {
	case "getMe":
		result = map[string]any{"username": "kokpit_test_bot"}
	case "getUpdates":
		f.mu.Lock()
		var batch []map[string]any
		if len(f.queue) > 0 {
			batch, f.queue = f.queue[0], f.queue[1:]
		}
		f.mu.Unlock()
		if batch == nil {
			time.Sleep(50 * time.Millisecond)
			batch = []map[string]any{}
		}
		result = batch
	case "sendMessage", "editMessageText":
		f.mu.Lock()
		f.sent = append(f.sent, p)
		f.mu.Unlock()
		result = map[string]any{"message_id": 1}
	}
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}

func msg(chat int64, text string) map[string]any {
	return map[string]any{"message": map[string]any{"message_id": 5, "text": text, "chat": map[string]any{"id": chat}}}
}

func cb(chat int64, data string) map[string]any {
	return map[string]any{"callback_query": map[string]any{"id": "c", "data": data, "message": map[string]any{"message_id": 9, "chat": map[string]any{"id": chat}}}}
}

func waitFor(t *testing.T, f *fake, sub string) string {
	t.Helper()
	for i := 0; i < 100; i++ {
		for _, s := range f.texts() {
			if strings.Contains(s, sub) {
				return s
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("%q içeren mesaj gelmedi; gelenler: %q", sub, f.texts())
	return ""
}

func TestBot(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{}
	set, err := settings.Open(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	set.Update(func(s *settings.Settings) {
		s.Telegram.Token, s.Telegram.ChatID, s.Telegram.Commands = "123:abc", "42", true
	})
	st, _ := store.Open(dir)
	up, _ := uptime.Open(dir)
	dep, _ := deploy.Open(dir)
	p, err := dep.Save(deploy.Project{Name: "Deneme", Dir: dir, Steps: []deploy.Step{{Cmd: "true"}}, Telegram: true})
	if err != nil {
		t.Fatal(err)
	}
	dep.Save(deploy.Project{Name: "Gizli", Dir: dir, Steps: []deploy.Step{{Cmd: "true"}}}) // Telegram'a kapalı

	f := &fake{}
	srv := httptest.NewServer(f)
	defer srv.Close()
	b := New(set, st, system.New(st), services.New(cfg), up, dep)
	b.API = srv.URL
	b.Start()

	// yetkisiz sohbet: yanıt verilmez
	f.push(msg(999, "/durum"))
	time.Sleep(300 * time.Millisecond)
	if len(f.texts()) != 0 || b.Status().Ignored != 1 {
		t.Fatalf("yetkisiz sohbete yanıt verildi: %q", f.texts())
	}
	f.push(msg(42, "/durum"))
	waitFor(t, f, "CPU:")

	// deploy: menü → seçim → onay
	f.push(msg(42, "/deploy"))
	m := waitFor(t, f, "Hangi proje")
	_ = m
	data := f.lastKeyboardData()
	if len(data) != 1 { // sadece Telegram'a açık proje
		t.Fatalf("menü: %v", data)
	}
	// yetkisiz sohbet butona basamaz
	f.push(cb(999, data[0]))
	f.push(cb(42, data[0]))
	waitFor(t, f, "deploy edilsin mi")
	conf := f.lastKeyboardData()
	if len(conf) != 2 || !strings.HasPrefix(conf[0], "y:") {
		t.Fatalf("onay: %v", conf)
	}
	f.push(cb(42, conf[0]))
	waitFor(t, f, "deploy edildi")
	if runs := dep.Runs(p.ID, 5); len(runs) != 1 || runs[0].Trigger != "telegram" || runs[0].Status != "ok" {
		t.Fatalf("deploy kaydı: %+v", runs)
	}
	// aynı onay ikinci kez kullanılamaz
	f.push(cb(42, conf[0]))
	waitFor(t, f, "süresi doldu")
	if len(dep.Runs(p.ID, 5)) != 1 {
		t.Fatal("onay iki kez kullanıldı")
	}
	// komutlar kapatılınca yanıt vermez
	set.Update(func(s *settings.Settings) { s.Telegram.Commands = false })
	n := len(f.texts())
	f.push(msg(42, "/durum"))
	time.Sleep(500 * time.Millisecond)
	if len(f.texts()) != n {
		t.Fatal("kapalıyken yanıt verdi")
	}
}
