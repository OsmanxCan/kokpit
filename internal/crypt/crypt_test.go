package crypt

import (
	"encoding/base32"
	"testing"
	"time"
)

// RFC 6238 test vektörü (SHA1, anahtar "12345678901234567890")
func TestTOTPRFC(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	cases := map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"}
	for ts, want := range cases {
		got, _ := TOTPCode(secret, time.Unix(ts, 0))
		if got != want {
			t.Fatalf("t=%d: %s != %s", ts, got, want)
		}
	}
}

func TestPassword(t *testing.T) {
	h, err := HashPassword("cok-gizli-sifre-123")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "cok-gizli-sifre-123") || CheckPassword(h, "yanlis") {
		t.Fatal("şifre doğrulama hatalı")
	}
}
