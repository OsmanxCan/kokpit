# Katkıda Bulunma

Katkıların için teşekkürler! Geliştirme ortamı ve proje yapısı için [GELISTIRME.md](GELISTIRME.md)'ye bak.

- Hata bildirimi veya özellik önerisi için issue aç
- Büyük değişikliklerden önce issue'da konuşalım
- Güvenlik açıkları için **issue açma**, [SECURITY.md](SECURITY.md)'ye bak
- Go tarafında dış paket eklemekten kaçın. Proje bilerek sadece standart kütüphaneyle yazıldı
- Arayüz metinleri Türkçe, sade ve açık olsun

Pull request göndermeden önce:

```bash
gofmt -l .                     # boş çıktı vermeli
go vet ./... && go test ./...
cd web && npm run build
```
