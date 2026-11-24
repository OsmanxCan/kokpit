package settings

import "testing"

func TestRender(t *testing.T) {
	got := Render("<b>{{servis}}</b> & <x> {{yok}} {{ip}}", map[string]string{"servis": "<nginx>", "ip": "1.2.3.4"}, "h")
	want := "<b>&lt;nginx&gt;</b> &amp; &lt;x&gt; {{yok}} 1.2.3.4"
	if got != want {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
	// dengesiz etiket → hepsi düz metin
	if got := Render("<b>açık kaldı", nil, "h"); got != "&lt;b&gt;açık kaldı" {
		t.Fatalf("%q", got)
	}
	if Render("{{sunucu}}", nil, "ubuntu") != "ubuntu" {
		t.Fatal("sunucu")
	}
	for _, e := range Events {
		if r := Render(e.Template, SampleVars(e), "ubuntu"); varRe.MatchString(r) {
			t.Errorf("%s şablonunda doldurulmayan değişken: %s", e.ID, r)
		}
	}
}
