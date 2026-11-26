package nginx

import "testing"

func TestParse(t *testing.T) {
	src := `server {
    server_name repo.example.com; # yorum
    root /srv/repo;
    location / { try_files $uri $uri/ =404; }
    listen 443 ssl;
    ssl_certificate /etc/letsencrypt/live/repo.example.com/fullchain.pem;
}
server {
    listen 80;
    server_name app.example.com;
    location / { proxy_pass http://127.0.0.1:3000; }
    location /api { proxy_pass http://127.0.0.1:3000; }
}`
	s := parse(src)
	if len(s) != 2 || s[0].Names[0] != "repo.example.com" || !s[0].SSL || s[0].Root != "/srv/repo" {
		t.Fatalf("%+v", s)
	}
	if len(s[1].ProxyPass) != 1 || s[1].ProxyPass[0] != "http://127.0.0.1:3000" {
		t.Fatalf("%+v", s[1])
	}
}
