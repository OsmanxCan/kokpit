package settings

import (
	"html"
	"regexp"
	"strings"
	"time"
)

type Var struct {
	Name   string `json:"name"`
	Desc   string `json:"desc"`
	Sample string `json:"sample"`
}

// EventDef bir bildirim türünün tanımı.
type EventDef struct {
	ID        string `json:"id"`
	Group     string `json:"group"`
	Name      string `json:"name"`
	Desc      string `json:"desc"`
	Level     string `json:"level"` // critical | warning | info | ok
	DefaultOn bool   `json:"defaultOn"`
	Cooldown  int    `json:"defaultCooldown"` // dakika
	Template  string `json:"defaultTemplate"`
	Vars      []Var  `json:"vars"`
}

// Her şablonda kullanılabilen ortak değişkenler.
var CommonVars = []Var{
	{"sunucu", "Sunucunun adı (hostname)", "ubuntu"},
	{"tarih", "Gün.ay.yıl", "24.11.2026"},
	{"saat", "Saat:dakika", "15:42"},
}

var Events = []EventDef{
	// --- Sistem
	{ID: "cpu_high", Group: "Sistem", Name: "Yüksek CPU", Level: "warning", DefaultOn: true, Cooldown: 30,
		Desc:     "CPU kullanımı belirlenen süre boyunca eşiğin üstünde kaldığında",
		Template: "🟡 <b>CPU kullanımı yüksek</b>\n{{cpu}} · {{dakika}} dakikadır eşiğin (%{{esik}}) üstünde",
		Vars:     []Var{{"cpu", "Şu anki CPU kullanımı", "%94"}, {"dakika", "Kaç dakikadır yüksek", "3"}, {"esik", "Eşik değeri", "90"}}},
	{ID: "ram_high", Group: "Sistem", Name: "Yüksek RAM", Level: "warning", DefaultOn: true, Cooldown: 30,
		Desc:     "RAM kullanımı belirlenen süre boyunca eşiğin üstünde kaldığında",
		Template: "🟡 <b>RAM kullanımı yüksek</b>\n{{ram}} ({{kullanilan}} / {{toplam}}) · {{dakika}} dakikadır",
		Vars: []Var{{"ram", "RAM kullanım yüzdesi", "%92"}, {"kullanilan", "Kullanılan bellek", "1,8 GB"},
			{"toplam", "Toplam bellek", "1,9 GB"}, {"dakika", "Kaç dakikadır yüksek", "3"}, {"esik", "Eşik değeri", "90"}}},
	{ID: "disk_full", Group: "Sistem", Name: "Disk doluyor", Level: "critical", DefaultOn: true, Cooldown: 360,
		Desc:     "Bir diskin doluluğu eşiği geçtiğinde",
		Template: "🔴 <b>Disk dolmak üzere</b>\n{{disk}} · {{yuzde}} dolu ({{bos}} boş)",
		Vars:     []Var{{"disk", "Bağlama noktası", "/"}, {"yuzde", "Doluluk", "%93"}, {"bos", "Boş alan", "2,1 GB"}}},

	// --- Projeler
	{ID: "pm2_down", Group: "Projeler", Name: "PM2 süreci çöktü", Level: "critical", DefaultOn: true, Cooldown: 15,
		Desc:     "Bir PM2 süreci online durumdan çıktığında",
		Template: "🔴 <b>{{surec}} çalışmıyor</b>\nPM2 durumu: {{durum}}",
		Vars:     []Var{{"surec", "Süreç adı", "cantools"}, {"durum", "PM2 durumu", "errored"}}},
	{ID: "pm2_up", Group: "Projeler", Name: "PM2 süreci düzeldi", Level: "ok", DefaultOn: true, Cooldown: 1,
		Desc:     "Çöken bir PM2 süreci tekrar çalışmaya başladığında",
		Template: "🟢 <b>{{surec}}</b> tekrar çalışıyor",
		Vars:     []Var{{"surec", "Süreç adı", "cantools"}}},
	{ID: "service_down", Group: "Projeler", Name: "Servis çöktü", Level: "critical", DefaultOn: true, Cooldown: 15,
		Desc:     "İzlenen bir systemd servisi durduğunda",
		Template: "🔴 <b>{{servis}} servisi çalışmıyor</b>\nDurum: {{durum}}",
		Vars:     []Var{{"servis", "Servis adı", "nginx"}, {"durum", "systemd durumu", "failed/failed"}}},
	{ID: "docker_down", Group: "Projeler", Name: "Container durdu", Level: "warning", DefaultOn: true, Cooldown: 60,
		Desc:     "Bir Docker container'ı çalışmıyor durumdayken",
		Template: "🟡 <b>{{container}} container'ı çalışmıyor</b>\nDurum: {{durum}}",
		Vars:     []Var{{"container", "Container adı", "amnezia-xray"}, {"durum", "Docker durumu", "exited"}}},

	// --- Deploy
	{ID: "deploy_ok", Group: "Deploy", Name: "Deploy tamamlandı", Level: "ok", DefaultOn: true, Cooldown: 0,
		Desc:     "Bir proje başarıyla deploy edildiğinde",
		Template: "🚀 <b>{{proje}}</b> deploy edildi\n{{commit}} · {{mesaj}}\nSüre: {{sure}} · {{kim}}",
		Vars:     []Var{{"proje", "Proje adı", "CanTools"}, {"commit", "Yeni sürüm (kısa commit)", "2c83137"}, {"mesaj", "Commit mesajı", "Arama düzeltildi"}, {"sure", "Süre", "1 dk 12 sn"}, {"kim", "Başlatan", "Can (panel)"}}},
	{ID: "deploy_failed", Group: "Deploy", Name: "Deploy başarısız", Level: "critical", DefaultOn: true, Cooldown: 0,
		Desc:     "Deploy hata verdiğinde (otomatik geri alındıysa belirtilir)",
		Template: "❌ <b>{{proje}}</b> deploy başarısız\n{{hata}}\n{{durum}}",
		Vars:     []Var{{"proje", "Proje adı", "CanTools"}, {"hata", "Hata", "derle adımı başarısız: çıkış kodu 1"}, {"durum", "Geri alma durumu", "Önceki sürüme geri dönüldü"}, {"kim", "Başlatan", "Can (panel)"}}},

	// --- Uptime
	{ID: "site_down", Group: "Uptime", Name: "Site çöktü", Level: "critical", DefaultOn: true, Cooldown: 1,
		Desc:     "İzlenen bir site art arda 2 kontrolde yanıt vermediğinde",
		Template: "🔴 <b>{{site}} erişilemiyor</b>\nAdres: {{adres}}\nSebep: {{sebep}}",
		Vars:     []Var{{"site", "İzleyicinin adı", "CanTools"}, {"adres", "Kontrol edilen adres", "https://cantools.ocbstd.com"}, {"sebep", "Hata", "HTTP 502"}}},
	{ID: "site_up", Group: "Uptime", Name: "Site düzeldi", Level: "ok", DefaultOn: true, Cooldown: 1,
		Desc:     "Çöken bir site tekrar yanıt verdiğinde",
		Template: "🟢 <b>{{site}}</b> tekrar erişilebilir\nKesinti süresi: {{sure}}",
		Vars:     []Var{{"site", "İzleyicinin adı", "CanTools"}, {"adres", "Kontrol edilen adres", "https://cantools.ocbstd.com"}, {"sure", "Kesinti süresi", "4 dk"}}},
	{ID: "ssl_expiring", Group: "Uptime", Name: "SSL sertifikası bitiyor", Level: "warning", DefaultOn: true, Cooldown: 1440,
		Desc:     "İzlenen bir sitenin SSL sertifikasının bitmesine 14 gün veya daha az kaldığında",
		Template: "🟡 <b>{{site}}</b> SSL sertifikasının bitmesine <b>{{gun}} gün</b> kaldı",
		Vars:     []Var{{"site", "İzleyicinin adı", "CanTools"}, {"adres", "Kontrol edilen adres", "https://cantools.ocbstd.com"}, {"gun", "Kalan gün", "9"}}},

	// --- Güvenlik
	{ID: "ssh_new_ip", Group: "Güvenlik", Name: "Yeni IP'den SSH girişi", Level: "critical", DefaultOn: true, Cooldown: 1,
		Desc:     "Daha önce hiç görülmemiş bir IP adresinden SSH ile başarılı giriş yapıldığında",
		Template: "⚠️ <b>Yeni IP'den SSH girişi!</b>\nKullanıcı: {{kullanici}}\nIP: {{ip}}\nYöntem: {{yontem}}\n\nSen değilsen hemen müdahale et.",
		Vars:     []Var{{"kullanici", "Giriş yapan kullanıcı", "root"}, {"ip", "IP adresi", "203.0.113.7"}, {"yontem", "Giriş yöntemi", "publickey"}}},
	{ID: "ssh_login", Group: "Güvenlik", Name: "SSH girişi", Level: "info", DefaultOn: true, Cooldown: 30,
		Desc:     "Bilinen bir IP'den SSH ile giriş yapıldığında",
		Template: "🔵 SSH girişi: {{kullanici}} @ {{ip}} ({{yontem}})",
		Vars:     []Var{{"kullanici", "Giriş yapan kullanıcı", "root"}, {"ip", "IP adresi", "78.173.36.157"}, {"yontem", "Giriş yöntemi", "publickey"}}},
	{ID: "ssh_brute", Group: "Güvenlik", Name: "Yoğun SSH saldırısı", Level: "warning", DefaultOn: true, Cooldown: 60,
		Desc:     "Bir dakika içindeki başarısız SSH denemeleri eşiği geçtiğinde",
		Template: "🟡 <b>Yoğun SSH saldırısı</b>\nSon dakikada {{sayi}} başarısız deneme\nEn çok deneyenler: {{ipler}}",
		Vars:     []Var{{"sayi", "Başarısız deneme sayısı", "84"}, {"ipler", "En çok deneyen IP'ler", "45.12.8.3 (40), 103.2.1.9 (22)"}}},
	{ID: "panel_login", Group: "Güvenlik", Name: "Panele giriş", Level: "info", DefaultOn: true, Cooldown: 30,
		Desc:     "Kokpit'e başarıyla giriş yapıldığında",
		Template: "🔵 Kokpit'e giriş yapıldı: {{kullanici}} @ {{ip}}",
		Vars:     []Var{{"kullanici", "Panel kullanıcısı", "Can"}, {"ip", "IP adresi", "78.173.36.157"}}},
	{ID: "terminal_open", Group: "Güvenlik", Name: "Web terminali açıldı", Level: "warning", DefaultOn: true, Cooldown: 0,
		Desc:     "Panelden sunucuda bir terminal (root kabuğu) açıldığında",
		Template: "🖥 <b>{{kullanici}}</b> panelden terminal açtı\nIP: {{ip}}",
		Vars:     []Var{{"kullanici", "Panel kullanıcısı", "Can"}, {"ip", "IP adresi", "78.173.36.157"}}},
	{ID: "panel_login_fail", Group: "Güvenlik", Name: "Hatalı panel girişi", Level: "warning", DefaultOn: true, Cooldown: 10,
		Desc:     "Bir IP panele 3 veya daha fazla kez hatalı giriş denediğinde",
		Template: "🟡 <b>Kokpit'e hatalı giriş denemesi</b>\nIP: {{ip}} · {{sayi}} deneme\nDenenen kullanıcı: {{kullanici}}",
		Vars:     []Var{{"ip", "IP adresi", "198.51.100.23"}, {"sayi", "Deneme sayısı", "3"}, {"kullanici", "Denenen kullanıcı adı", "admin"}}},

	// --- Kokpit
	{ID: "panel_start", Group: "Kokpit", Name: "Kokpit başladı", Level: "ok", DefaultOn: true, Cooldown: 1,
		Desc:     "Kokpit servisi başlatıldığında veya yeniden başlatıldığında",
		Template: "🟢 Kokpit {{surum}} başlatıldı",
		Vars:     []Var{{"surum", "Kokpit sürümü", "0.2.0"}}},
	{ID: "update_available", Group: "Kokpit", Name: "Yeni sürüm var", Level: "info", DefaultOn: true, Cooldown: 1440,
		Desc:     "GitHub'da Kokpit'in yeni bir sürümü yayınlandığında",
		Template: "🔵 <b>Kokpit {{yeni}} yayında</b> (kurulu: {{kurulu}})\nPanelde Güncellemeler sayfasından kurabilirsin.",
		Vars:     []Var{{"yeni", "Yeni sürüm", "0.3.0"}, {"kurulu", "Kurulu sürüm", "0.2.0"}}},
	{ID: "update_done", Group: "Kokpit", Name: "Güncelleme kuruldu", Level: "ok", DefaultOn: true, Cooldown: 1,
		Desc:     "Kokpit yeni bir sürüme güncellendiğinde",
		Template: "🟢 Kokpit güncellendi: {{eski}} → {{yeni}}",
		Vars:     []Var{{"eski", "Önceki sürüm", "0.2.0"}, {"yeni", "Yeni sürüm", "0.3.0"}}},
	{ID: "test", Group: "Kokpit", Name: "Test bildirimi", Level: "info", DefaultOn: true, Cooldown: 0,
		Desc:     "Ayarlardaki “Test bildirimi gönder” butonuyla",
		Template: "✅ Test bildirimi: Kokpit bildirimleri çalışıyor.",
		Vars:     []Var{}},
}

func EventByID(id string) (EventDef, bool) {
	for _, e := range Events {
		if e.ID == id {
			return e, true
		}
	}
	return EventDef{}, false
}

// SampleVars bir olayın örnek değişken değerleri (önizleme için).
func SampleVars(d EventDef) map[string]string {
	m := map[string]string{}
	for _, v := range CommonVars {
		m[v.Name] = v.Sample
	}
	for _, v := range d.Vars {
		m[v.Name] = v.Sample
	}
	return m
}

var varRe = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_çğıöşüÇĞİÖŞÜ]+)\s*\}\}`)

// Telegram'ın HTML modunda desteklenen etiketler
var allowedTag = regexp.MustCompile(`(?i)</?(b|i|u|s|code|pre)>`)

// Render şablonu Telegram HTML'ine çevirir:
//   - <b> <i> <u> <s> <code> <pre> etiketleri korunur (dengeli değilse hepsi düz metin olur)
//   - geri kalan her şey (&, <, >) kaçışlanır
//   - {{degisken}} yerleri kaçışlanmış değerlerle doldurulur
func Render(tpl string, vars map[string]string, host string) string {
	now := time.Now()
	all := map[string]string{"sunucu": host, "tarih": now.Format("02.01.2006"), "saat": now.Format("15:04")}
	for k, v := range vars {
		all[k] = v
	}
	keepTags := tagsBalanced(tpl)
	var b strings.Builder
	last := 0
	if keepTags {
		for _, loc := range allowedTag.FindAllStringIndex(tpl, -1) {
			b.WriteString(html.EscapeString(tpl[last:loc[0]]))
			b.WriteString(strings.ToLower(tpl[loc[0]:loc[1]]))
			last = loc[1]
		}
	}
	b.WriteString(html.EscapeString(tpl[last:]))
	return varRe.ReplaceAllStringFunc(b.String(), func(m string) string {
		name := varRe.FindStringSubmatch(m)[1]
		if v, ok := all[name]; ok {
			return html.EscapeString(v)
		}
		return m
	})
}

func tagsBalanced(t string) bool {
	var stack []string
	for _, m := range allowedTag.FindAllString(t, -1) {
		m = strings.ToLower(m)
		if strings.HasPrefix(m, "</") {
			name := m[2 : len(m)-1]
			if len(stack) == 0 || stack[len(stack)-1] != name {
				return false
			}
			stack = stack[:len(stack)-1]
		} else {
			stack = append(stack, m[1:len(m)-1])
		}
	}
	return len(stack) == 0
}

var tagRe = regexp.MustCompile(`<[^>]*>`)

// Plain Telegram HTML'ini düz metne çevirir (panelde listelemek için).
func Plain(s string) string {
	return strings.TrimSpace(html.UnescapeString(tagRe.ReplaceAllString(s, "")))
}

// CleanTemplate kaydetmeden önce şablonu düzenler.
func CleanTemplate(t string) string {
	t = strings.ReplaceAll(t, "\r\n", "\n")
	if len([]rune(t)) > 2000 {
		t = string([]rune(t)[:2000])
	}
	return strings.TrimSpace(t)
}
