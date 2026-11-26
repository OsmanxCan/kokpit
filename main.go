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
	"regexp"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/OsmanxCan/kokpit/internal/alerts"
	"github.com/OsmanxCan/kokpit/internal/cloudflare"
	"github.com/OsmanxCan/kokpit/internal/config"
	"github.com/OsmanxCan/kokpit/internal/crypt"
	"github.com/OsmanxCan/kokpit/internal/deploy"
	"github.com/OsmanxCan/kokpit/internal/firewall"
	"github.com/OsmanxCan/kokpit/internal/geoip"
	"github.com/OsmanxCan/kokpit/internal/logrules"
	"github.com/OsmanxCan/kokpit/internal/ports"
	"github.com/OsmanxCan/kokpit/internal/portscan"
	"github.com/OsmanxCan/kokpit/internal/security"
	"github.com/OsmanxCan/kokpit/internal/server"
	"github.com/OsmanxCan/kokpit/internal/services"
	"github.com/OsmanxCan/kokpit/internal/settings"
	"github.com/OsmanxCan/kokpit/internal/sitewizard"
	"github.com/OsmanxCan/kokpit/internal/store"
	"github.com/OsmanxCan/kokpit/internal/system"
	"github.com/OsmanxCan/kokpit/internal/telegram"
	"github.com/OsmanxCan/kokpit/internal/update"
	"github.com/OsmanxCan/kokpit/internal/uptime"
	"github.com/OsmanxCan/kokpit/internal/weblog"
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

	// varsayılan depolama klasörü (dosya gezgini) yoksa oluştur
	for _, r := range set.Get().FileRoots {
		if r.Path == settings.DefaultStorage {
			os.MkdirAll(r.Path, 0o750)
		}
	}

	up, err := uptime.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("uptime verisi okunamadı: %v", err)
	}
	up.OnEvent = func(ev uptime.Event) {
		m := ev.Monitor
		vars := map[string]string{"site": m.Name, "adres": m.URL}
		switch ev.Kind {
		case "down":
			vars["sebep"] = ev.Reason
			al.Fire("site_down", m.ID, vars)
		case "up":
			vars["sure"] = humanDuration(ev.Down)
			al.Fire("site_up", m.ID, vars)
		case "ssl":
			vars["gun"] = fmt.Sprint(ev.Days)
			al.Fire("ssl_expiring", m.ID, vars)
		}
	}
	up.Start()

	geo := geoip.Open(cfg.DataDir)
	geo.Start()

	dep, err := deploy.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("deploy verisi okunamadı: %v", err)
	}
	dep.Restart = func(name string) error {
		if _, err := svc.Action("pm2", name, "reload"); err != nil {
			out, err2 := svc.Action("pm2", name, "restart")
			if err2 != nil {
				return fmt.Errorf("%v %s", err2, out)
			}
		}
		return nil
	}
	dep.OnFinish = func(ev deploy.Event) {
		r := ev.Run
		kim := r.User + " (" + map[string]string{"telegram": "Telegram"}[r.Trigger] + ")"
		if r.Trigger != "telegram" {
			kim = r.User + " (panel)"
		}
		vars := map[string]string{"proje": ev.Project.Name, "kim": kim}
		switch r.Status {
		case "ok":
			vars["commit"] = shortCommit(r.To)
			vars["mesaj"] = r.Message
			vars["sure"] = humanDuration(time.Duration(r.End-r.Start) * time.Second)
			al.Fire("deploy_ok", r.ID, vars)
		case "failed", "rolled_back":
			vars["hata"] = r.Error
			vars["durum"] = map[bool]string{true: "Önceki sürüme geri dönüldü", false: "Geri alınmadı"}[r.Status == "rolled_back"]
			al.Fire("deploy_failed", r.ID, vars)
		}
	}

	bot := telegram.New(set, st, sys, svc, up, dep)
	bot.Start()

	// port tarama tespiti
	ps := portscan.Open(cfg.DataDir, func() settings.PortScan { return set.Get().PortScan })
	ps.Country = geo.Country
	ps.BanFunc = firewall.Ban
	ps.UnbanFunc = firewall.Unban
	ps.Protected = func(ip string) bool {
		for _, s := range st.Sessions() {
			if s.IP == ip {
				return true
			}
		}
		return false
	}
	ps.SSHTried = func(ip string) bool {
		tried := false
		security.SSHScan("-1h", regexp.QuoteMeta(ip), func(e security.SSHEvent) {
			if e.IP == ip && e.Type != "accepted" {
				tried = true
			}
		})
		return tried
	}
	ps.OnScan = func(sc portscan.Scan) {
		engel := ""
		if sc.Banned {
			h := set.Get().PortScan.BanHours
			engel = "\n🚫 Kalıcı olarak engellendi"
			if h > 0 {
				engel = fmt.Sprintf("\n🚫 %d saat engellendi", h)
			}
		}
		al.Fire("port_scan", sc.IP, map[string]string{"ip": sc.IP, "ulke": flagCC(sc.CC), "port": fmt.Sprint(sc.Ports),
			"tur": strings.Join(sc.Types, ", "), "neden": sc.Reason, "engel": engel})
	}
	ps.Run()

	// dışarıya açık portlar
	pw := ports.NewWatcher()
	pw.OnNew = func(it ports.Item) {
		risk := ""
		if it.Risk != "" {
			risk = "\n⚠️ " + it.Risk + " genelde dışarıya açık olmamalı"
		}
		al.Fire("port_open", it.Key(), map[string]string{"port": fmt.Sprintf("%d/%s", it.Port, it.Proto), "program": orDash(it.Program),
			"neden": it.Reason, "risk": risk})
	}
	pw.Start()

	// Cloudflare (isteğe bağlı) ve ortak IP engelleme
	cf := cloudflare.Open(cfg.DataDir, func() string { return set.Get().CFToken })
	cf.Start()
	banAny := func(ip, reason string, hours int, action, by string) (string, error) {
		if why := ps.WhyProtected(ip); why != "" {
			return "", fmt.Errorf("%s engellenemez: %s", ip, why)
		}
		useCF := action == "cloudflare" || (action == "auto" && cf.Enabled())
		useFW := action == "firewall" || action == "auto"
		if action == "cloudflare" && !cf.Enabled() {
			return "", fmt.Errorf("Cloudflare ayarlanmamış")
		}
		var done []string
		var lastErr error
		if useCF {
			if err := cf.BanIP(ip, reason, hours, by); err != nil {
				lastErr = err
			} else {
				done = append(done, "Cloudflare")
			}
		}
		if useFW {
			if !firewall.Active() {
				if !useCF {
					lastErr = fmt.Errorf("UFW kapalı olduğu için firewall'da engellenemedi")
				}
			} else if err := ps.Ban(ip, reason, hours, by); err != nil {
				lastErr = err
			} else {
				done = append(done, "firewall")
			}
		}
		if len(done) == 0 {
			return "", lastErr
		}
		return strings.Join(done, " ve "), nil
	}

	// web logları
	wl := weblog.NewAnalyzer()
	wl.Country = geo.Country
	wl.Blocked = func(ip string) string {
		if cf.Banned(ip) {
			return "cloudflare"
		}
		for _, b := range ps.Bans() {
			if b.IP == ip {
				return "firewall"
			}
		}
		return ""
	}
	wl.OnFind = func(f weblog.Finding) {
		al.Fire("web_exposed", f.URL, map[string]string{"adres": f.URL, "site": f.Site})
	}
	go func() { // saatte bir arka planda analiz et (sızıntı bildirimi için)
		time.Sleep(2 * time.Minute)
		for {
			wl.Report("24h", true)
			time.Sleep(time.Hour)
		}
	}()

	// uygulama log kuralları
	lr := logrules.New(func() []settings.LogRule { return set.Get().LogRules })
	lr.PM2Logs = func(name string) []string { f, _ := svc.PM2LogFiles(name); return f }
	lr.Ban = func(ip, reason string, hours int, action string) (string, error) {
		return banAny(ip, "log kuralı: "+reason, hours, action, "log kuralı")
	}
	lr.OnHit = func(r settings.LogRule, h logrules.Hit) {
		al.Fire("app_ban", r.ID+h.IP, map[string]string{"kural": r.Name, "ip": h.IP, "ulke": flagCC(geo.Country(h.IP)),
			"sayi": fmt.Sprint(h.Count), "sonuc": h.Result})
	}
	lr.Start()

	srv := server.New(cfg, st, set, sys, svc, sec, al, upd, web.Dist(), up, geo, dep, bot, ps, pw)
	srv.Web, srv.CF, srv.Rules, srv.BanAny = wl, cf, lr, banAny
	srv.Wizard = sitewizard.New(cfg.DataDir, sitewizard.Deps{
		CFEnabled: cf.Enabled, EnsureCNAME: cf.EnsureCNAME, DeleteRecord: cf.DeleteRecord,
		AddMonitor: func(name, url string) (string, error) {
			m, err := up.Add(uptime.Monitor{Name: name, URL: url, Enabled: true})
			return m.ID, err
		},
		DelMonitor: up.Delete,
	})
	bot.CF = cf
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
	up.Flush()
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

// humanDuration "1 saat 5 dk" biçiminde süre.
func humanDuration(d time.Duration) string {
	m := int(d.Minutes())
	switch {
	case m < 1:
		return fmt.Sprintf("%d sn", int(d.Seconds()))
	case m < 60:
		return fmt.Sprintf("%d dk", m)
	case m < 1440:
		return fmt.Sprintf("%d saat %d dk", m/60, m%60)
	}
	return fmt.Sprintf("%d gün %d saat", m/1440, (m%1440)/60)
}

func shortCommit(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

// flagCC ülke kodunu bayrak ve kodla gösterir: "🇳🇱 NL".
func flagCC(cc string) string {
	if len(cc) != 2 {
		return "ülke bilinmiyor"
	}
	r := []rune(strings.ToUpper(cc))
	return string([]rune{0x1F1E6 + r[0] - 'A', 0x1F1E6 + r[1] - 'A'}) + " " + strings.ToUpper(cc)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
