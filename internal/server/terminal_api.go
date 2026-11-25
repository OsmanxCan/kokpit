package server

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"net/http"

	"github.com/OsmanxCan/kokpit/internal/terminal"
)

const (
	termIdle     = 15 * time.Minute // bu kadar süre tuşa basılmazsa oturum kapanır
	termMaxCount = 5                // aynı anda açık en fazla terminal
)

var termCount atomic.Int32

type termMsg struct {
	T    string `json:"t"` // i = giriş, r = boyut
	D    string `json:"d"`
	Cols int    `json:"c"`
	Rows int    `json:"r"`
}

// handleTerminal tarayıcıdaki xterm ile sunucuda bir kabuk arasında köprü kurar.
func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	if s.set.Get().TerminalOff {
		writeErr(w, 403, "Web terminali Ayarlar'dan kapatılmış")
		return
	}
	if termCount.Load() >= termMaxCount {
		writeErr(w, 429, fmt.Sprintf("Aynı anda en fazla %d terminal açılabilir", termMaxCount))
		return
	}
	conn, err := terminal.Upgrade(w, r)
	if err != nil {
		return
	}
	termCount.Add(1)
	defer termCount.Add(-1)

	si := sess(r)
	ip := clientIP(r)
	shell := "/bin/bash"
	if _, err := os.Stat(shell); err != nil {
		shell = "/bin/sh"
	}
	home := os.Getenv("HOME")
	if home == "" {
		home = "/root"
	}
	lang := os.Getenv("LANG")
	if lang == "" {
		lang = "C.UTF-8"
	}
	env := []string{"TERM=xterm-256color", "COLORTERM=truecolor", "HOME=" + home, "SHELL=" + shell, "LANG=" + lang,
		"USER=root", "LOGNAME=root", "KOKPIT_USER=" + si.Username,
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/snap/bin"}
	cols, rows := 100, 30
	cmd, pty, err := terminal.Start(shell, home, env, cols, rows)
	if err != nil {
		conn.WriteText([]byte(`{"error":"` + err.Error() + `"}`))
		conn.Close(1011, "terminal açılamadı")
		return
	}
	start := time.Now()
	s.st.Audit(si.Username, ip, "terminal aç", shell, true, "")
	s.alerts.Fire("terminal_open", si.Username+ip, map[string]string{"kullanici": si.Username, "ip": ip})

	var once sync.Once
	reason := "kapatıldı"
	closeAll := func(why string) {
		once.Do(func() {
			reason = why
			cmd.Process.Signal(syscall.SIGHUP)
			pty.Close()
		})
	}
	var lastInput atomic.Int64
	lastInput.Store(time.Now().UnixNano())

	// PTY → tarayıcı
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := pty.Read(buf)
			if n > 0 {
				if conn.WriteBinary(buf[:n]) != nil {
					closeAll("bağlantı koptu")
					conn.Close(1001, "")
					return
				}
			}
			if err != nil {
				closeAll("kabuk kapandı")
				conn.Close(1000, "exit")
				return
			}
		}
	}()
	// boşta kalma, oturum geçerliliği ve canlı tutma
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if time.Since(time.Unix(0, lastInput.Load())) > termIdle {
					conn.WriteBinary([]byte("\r\n\x1b[33m15 dakika işlem yapılmadığı için terminal kapatıldı.\x1b[0m\r\n"))
					closeAll("boşta kaldı")
					conn.Close(4000, "idle")
					return
				}
				if _, ok := s.session(r); !ok {
					closeAll("oturum sona erdi")
					conn.Close(4001, "session")
					return
				}
				if s.set.Get().TerminalOff {
					closeAll("terminal kapatıldı")
					conn.Close(4003, "disabled")
					return
				}
				conn.Ping()
			}
		}
	}()
	// tarayıcı → PTY
	for {
		op, data, err := conn.Read()
		if err != nil {
			closeAll("bağlantı kapandı")
			break
		}
		if op != 1 {
			continue
		}
		var m termMsg
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.T {
		case "i":
			lastInput.Store(time.Now().UnixNano())
			if _, err := pty.Write([]byte(m.D)); err != nil {
				closeAll("kabuk kapandı")
			}
		case "r":
			terminal.Resize(pty, m.Cols, m.Rows)
		}
	}
	close(stop)
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		cmd.Process.Kill()
		<-done
	}
	conn.Close(1000, "")
	s.st.Audit(si.Username, ip, "terminal kapat", fmt.Sprintf("%s, %s", reason, time.Since(start).Round(time.Second)), true, "")
}
