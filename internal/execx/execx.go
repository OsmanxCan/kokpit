// Package execx komut çalıştırma yardımcıları. Komutlar her zaman shell OLMADAN
// (exec.Command ile) çalıştırılır, böylece komut enjeksiyonu yapılamaz.
package execx

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Run komutu varsayılan 20 sn zaman aşımıyla çalıştırır, stdout+stderr döner.
func Run(name string, args ...string) (string, error) {
	return RunTimeout(20*time.Second, name, args...)
}

func RunTimeout(d time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return out.String(), errors.New("komut zaman aşımına uğradı")
	}
	return out.String(), err
}

// Exists komut sistemde var mı?
func Exists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

var safeName = regexp.MustCompile(`^[A-Za-z0-9@._:\-]{1,128}$`)

// SafeName servis/süreç adlarının güvenli olup olmadığını kontrol eder.
// "-" ile başlayan adlar reddedilir (komut seçeneği gibi yorumlanmasın diye).
func SafeName(s string) bool {
	return safeName.MatchString(s) && !strings.HasPrefix(s, "-")
}
