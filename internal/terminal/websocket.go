package terminal

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Basit bir WebSocket (RFC 6455) sunucu uygulaması: metin/ikili çerçeveler,
// ping/pong ve kapatma. Parçalı (fragmented) mesajları birleştirir.

const (
	opCont   = 0x0
	opText   = 0x1
	opBinary = 0x2
	opClose  = 0x8
	opPing   = 0x9
	opPong   = 0xA
)

const maxMessage = 1 << 20

type Conn struct {
	c   net.Conn
	br  *bufio.Reader
	wmu sync.Mutex
}

var ErrBadHandshake = errors.New("geçersiz WebSocket isteği")

// Upgrade HTTP isteğini WebSocket bağlantısına çevirir. Origin başlığı Host ile
// aynı değilse reddeder (başka sitelerin bağlantı açmasını engeller).
func Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if !headerHas(r.Header, "Connection", "upgrade") || !headerHas(r.Header, "Upgrade", "websocket") ||
		r.Header.Get("Sec-WebSocket-Version") != "13" || r.Header.Get("Sec-WebSocket-Key") == "" {
		http.Error(w, "WebSocket bekleniyor", http.StatusBadRequest)
		return nil, ErrBadHandshake
	}
	if o := r.Header.Get("Origin"); o != "" {
		host := strings.TrimPrefix(strings.TrimPrefix(o, "https://"), "http://")
		if host != r.Host {
			http.Error(w, "Kaynak reddedildi", http.StatusForbidden)
			return nil, errors.New("origin uyuşmuyor: " + o)
		}
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "desteklenmiyor", http.StatusInternalServerError)
		return nil, errors.New("hijack desteklenmiyor")
	}
	h := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	c, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " +
		base64.StdEncoding.EncodeToString(h[:]) + "\r\n\r\n"
	if _, err := c.Write([]byte(resp)); err != nil {
		c.Close()
		return nil, err
	}
	c.SetDeadline(time.Time{})
	return &Conn{c: c, br: brw.Reader}, nil
}

func headerHas(h http.Header, k, v string) bool {
	for _, x := range h.Values(k) {
		for _, p := range strings.Split(x, ",") {
			if strings.EqualFold(strings.TrimSpace(p), v) {
				return true
			}
		}
	}
	return false
}

// Read bir sonraki veri mesajını döner; ping'lere kendisi yanıt verir.
func (c *Conn) Read() (op byte, data []byte, err error) {
	var msg []byte
	var msgOp byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(c.br, h[:]); err != nil {
			return 0, nil, err
		}
		fin := h[0]&0x80 != 0
		o := h[0] & 0x0f
		masked := h[1]&0x80 != 0
		n := uint64(h[1] & 0x7f)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(c.br, b[:]); err != nil {
				return 0, nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(c.br, b[:]); err != nil {
				return 0, nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if !masked {
			return 0, nil, errors.New("istemci çerçevesi maskesiz")
		}
		if n > maxMessage || uint64(len(msg))+n > maxMessage {
			return 0, nil, errors.New("mesaj çok büyük")
		}
		var mask [4]byte
		if _, err := io.ReadFull(c.br, mask[:]); err != nil {
			return 0, nil, err
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(c.br, p); err != nil {
			return 0, nil, err
		}
		for i := range p {
			p[i] ^= mask[i%4]
		}
		switch o {
		case opPing:
			c.write(opPong, p)
			continue
		case opPong:
			continue
		case opClose:
			c.write(opClose, nil)
			return opClose, nil, io.EOF
		case opText, opBinary:
			msgOp, msg = o, p
		case opCont:
			msg = append(msg, p...)
		default:
			return 0, nil, errors.New("bilinmeyen çerçeve")
		}
		if fin {
			return msgOp, msg, nil
		}
	}
}

func (c *Conn) write(op byte, p []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	h := []byte{0x80 | op}
	switch n := len(p); {
	case n < 126:
		h = append(h, byte(n))
	case n < 1<<16:
		h = append(h, 126, byte(n>>8), byte(n))
	default:
		h = append(h, 127)
		h = binary.BigEndian.AppendUint64(h, uint64(n))
	}
	c.c.SetWriteDeadline(time.Now().Add(30 * time.Second))
	if _, err := c.c.Write(append(h, p...)); err != nil {
		return err
	}
	return nil
}

func (c *Conn) WriteText(p []byte) error   { return c.write(opText, p) }
func (c *Conn) WriteBinary(p []byte) error { return c.write(opBinary, p) }
func (c *Conn) Ping() error                { return c.write(opPing, nil) }

// Close kapatma çerçevesi gönderip bağlantıyı kapatır.
func (c *Conn) Close(code uint16, reason string) {
	p := binary.BigEndian.AppendUint16(nil, code)
	if len(reason) > 120 {
		reason = reason[:120]
	}
	c.write(opClose, append(p, reason...))
	c.c.Close()
}
