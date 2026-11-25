// Package terminal tarayıcıdan kullanılan bir kabuk oturumu (PTY) sağlar.
// Harici paket kullanmadan /dev/ptmx üzerinden sözde terminal açar.
package terminal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"
)

// openPTY bir ana/yardımcı terminal çifti açar.
func openPTY() (master, slave *os.File, err error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	var unlock int32
	if err := ioctl(m.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); err != nil {
		m.Close()
		return nil, nil, err
	}
	var n uint32
	if err := ioctl(m.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); err != nil {
		m.Close()
		return nil, nil, err
	}
	s, err := os.OpenFile("/dev/pts/"+strconv.Itoa(int(n)), os.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		m.Close()
		return nil, nil, err
	}
	return m, s, nil
}

func ioctl(fd, req, arg uintptr) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg); e != 0 {
		return e
	}
	return nil
}

type winsize struct{ Rows, Cols, X, Y uint16 }

// Resize terminal boyutunu değiştirir.
func Resize(f *os.File, cols, rows int) error {
	if cols < 1 || rows < 1 || cols > 1000 || rows > 1000 {
		return errors.New("geçersiz boyut")
	}
	ws := winsize{Rows: uint16(rows), Cols: uint16(cols)}
	return ioctl(f.Fd(), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws)))
}

// Start kabuğu yeni bir PTY içinde başlatır.
func Start(shell, dir string, env []string, cols, rows int) (*exec.Cmd, *os.File, error) {
	m, s, err := openPTY()
	if err != nil {
		return nil, nil, fmt.Errorf("terminal açılamadı: %v", err)
	}
	defer s.Close()
	Resize(m, cols, rows)
	cmd := exec.Command(shell, "-l")
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = s, s, s
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		m.Close()
		return nil, nil, err
	}
	return cmd, m, nil
}
