// Kokpit — sunucu durumu, projeler ve güvenlik için kontrol paneli.
// Sadece Go standart kütüphanesi kullanır; dış bağımlılık yoktur.
//
// Kullanım:
//
//	kokpit                 → paneli başlatır
//	kokpit kullanici-ekle  → kullanıcı oluşturur / şifre ve 2FA'yı sıfırlar
//	kokpit guncelle        → GitHub'dan son sürümü indirip kurar
//	kokpit surum           → sürümü yazar
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/OsmanxCan/kokpit/internal/alerts"
	"github.com/OsmanxCan/kokpit/internal/config"
	"github.com/OsmanxCan/kokpit/internal/crypt"
	"github.com/OsmanxCan/kokpit/internal/security"
	"github.com/OsmanxCan/kokpit/internal/server"
	"github.com/OsmanxCan/kokpit/internal/services"
	"github.com/OsmanxCan/kokpit/internal/settings"
	"github.com/OsmanxCan/kokpit/internal/store"
	"github.com/OsmanxCan/kokpit/internal/system"
	"github.com/OsmanxCan/kokpit/internal/update"
	"github.com/OsmanxCan/kokpit/web"
)

// Derleme sırasında -ldflags ile doldurulur (bkz. build.sh ve .github/workflows/release.yml)
var (
	Version = "dev"
	Repo    = "" // GitHub deposu: kullanici/depo
)

func main() {
	cfg := config.Load(Repo)
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "", "serve", "baslat":
		serve(cfg)
	case "kullanici-ekle", "user-add":
		userAdd(cfg)
	case "guncelle", "update":
		selfUpdate(cfg)
	case "surum", "version", "--version", "-v":
		fmt.Println("kokpit", Version)
		if cfg.Repo != "" {
			fmt.Println("depo: https://github.com/" + cfg.Repo)
		}
	default:
		fmt.Println("Kullanım: kokpit [baslat | kullanici-ekle | guncelle | surum]")
		os.Exit(1)
	}
}

func serve(cfg *config.Config) {
	// Küçük sunucularda belleği sınırlı tut (GOMEMLIMIT ortam değişkeni verilmişse o geçerli)
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(64 << 20)
	}
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("veri klasörü açılamadı: %v", err)
	}
	st.StartFlusher()
	if st.UserCount() == 0 {
		log.Println("UYARI: Henüz kullanıcı yok. 'kokpit kullanici-ekle' çalıştır.")
	}

	set, err := settings.Open(cfg.DataDir, cfg)
	if err != nil {
		log.Fatalf("ayarlar okunamadı: %v", err)
	}

	sys := system.New(st)
	sys.Start()
	svc := services.New(cfg)
	svc.Watched = set.Watched
	sec := security.New()
	sec.RootPM2 = func() []string {
		var out []string
		if items, err := svc.PM2(); err == nil {
			for _, it := range items {
				if it.User == "root" {
					out = append(out, it.Name)
				}
			}
		}
		return out
	}
	al := alerts.New(set, st, sys, svc)
	al.Version = Version
	al.Start()

	upd := update.New(Version, cfg.Repo, set.Get().UpdateCheck)
	upd.OnNew = func(r *update.Release) {
		al.Fire("update_available", r.Version, map[string]string{"yeni": r.Version, "kurulu": Version})
	}
	upd.Start()

	go func() { // güvenlik raporunu 5 dakikada bir tazele
		for {
			sec.Get(0)
			time.Sleep(5 * time.Minute)
		}
	}()

	srv := server.New(cfg, st, set, sys, svc, sec, al, upd, web.Dist())
	hs := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		log.Printf("Kokpit %s → http://%s", Version, cfg.Addr)
		if !strings.HasPrefix(cfg.Addr, "127.0.0.1:") && !strings.HasPrefix(cfg.Addr, "localhost:") {
			log.Println("UYARI: Panel 127.0.0.1 dışında bir adreste dinliyor! Dışarıya açık olabilir.")
		}
		if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hs.Shutdown(ctx)
	st.Flush()
}

func prompt(r *bufio.Reader, label string) string {
	fmt.Print(label)
	s, _ := r.ReadString('\n')
	return strings.TrimSpace(s)
}

// terminal: "curl ... | bash" ile çalıştırılınca stdin bir pipe olur; o durumda /dev/tty'den oku.
func terminal() *os.File {
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		return os.Stdin
	}
	if t, err := os.Open("/dev/tty"); err == nil {
		return t
	}
	return os.Stdin
}

// readPassword ekrana yazdırmadan şifre okur (stty ile).
func readPassword(r *bufio.Reader, tty *os.File, label string) string {
	fmt.Print(label)
	off := exec.Command("stty", "-echo")
	off.Stdin = tty
	off.Run()
	s, _ := r.ReadString('\n')
	on := exec.Command("stty", "echo")
	on.Stdin = tty
	on.Run()
	fmt.Println()
	return strings.TrimRight(s, "\r\n")
}

func userAdd(cfg *config.Config) {
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("veri klasörü açılamadı: %v", err)
	}
	tty := terminal()
	in := bufio.NewReader(tty)
	username := prompt(in, "Kullanıcı adı: ")
	if len(username) < 3 || strings.ContainsAny(username, " \t") {
		log.Fatal("kullanıcı adı en az 3 karakter olmalı ve boşluk içermemeli")
	}
	pw := readPassword(in, tty, "Şifre (en az 12 karakter): ")
	if len(pw) < 12 {
		log.Fatal("şifre en az 12 karakter olmalı")
	}
	if readPassword(in, tty, "Şifre (tekrar): ") != pw {
		log.Fatal("şifreler eşleşmiyor")
	}
	secret := crypt.NewTOTPSecret()
	url := crypt.TOTPURL("Kokpit", username, secret)
	fmt.Println("\n2FA kurulumu — Google Authenticator / Aegis / 2FAS uygulamasına ekle:")
	if _, err := exec.LookPath("qrencode"); err == nil {
		q := exec.Command("qrencode", "-t", "ansiutf8", url)
		q.Stdout = os.Stdout
		q.Run()
	} else {
		fmt.Println("(QR görmek için: apt install qrencode)")
	}
	// gruplu göster: okuması kolay olsun
	var groups []string
	for i := 0; i < len(secret); i += 4 {
		end := i + 4
		if end > len(secret) {
			end = len(secret)
		}
		groups = append(groups, secret[i:end])
	}
	fmt.Println("Elle giriş anahtarı:", strings.Join(groups, " "))
	fmt.Println("Tür: Zamana dayalı (TOTP), 6 hane, 30 sn")
	for i := 0; i < 3; i++ {
		code := prompt(in, "\nUygulamadaki 6 haneli kodu yaz: ")
		if _, err := crypt.ValidateTOTP(secret, code); err == nil {
			hash, err := crypt.HashPassword(pw)
			if err != nil {
				log.Fatal(err)
			}
			if err := st.UpsertUser(store.User{Username: username, PassHash: hash, TOTPSecret: secret}); err != nil {
				log.Fatal(err)
			}
			fmt.Println("✓ Kullanıcı kaydedildi:", username, "(bu kullanıcının eski oturumları kapatıldı)")
			return
		}
		fmt.Println("✗ Kod hatalı, tekrar dene.")
	}
	log.Fatal("2FA doğrulanamadı, kullanıcı kaydedilmedi")
}

func selfUpdate(cfg *config.Config) {
	if cfg.Repo == "" {
		log.Fatal("GitHub deposu bilinmiyor. PANEL_REPO=kullanici/depo ayarla.")
	}
	u := update.New(Version, cfg.Repo, true)
	fmt.Println("Kurulu sürüm:", Version, "· depo:", cfg.Repo)
	r, err := u.Check()
	if err != nil {
		log.Fatal(err)
	}
	if !update.Newer(r.Version, Version) {
		fmt.Println("✓ Zaten en güncel sürüm:", r.Version)
		return
	}
	fmt.Println("Yeni sürüm:", r.Version, "→ indiriliyor ve doğrulanıyor…")
	ver, err := u.Apply()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("✓ Güncellendi:", ver)
	if out, err := exec.Command("systemctl", "restart", "kokpit").CombinedOutput(); err != nil {
		fmt.Println("Servisi elle yeniden başlat: systemctl restart kokpit", strings.TrimSpace(string(out)))
	} else {
		fmt.Println("✓ Servis yeniden başlatıldı")
	}
}
