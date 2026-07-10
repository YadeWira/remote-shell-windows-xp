package transport

import (
	"net"
	"sync"
	"time"

	"github.com/BaseMax/remote-shell-windows-xp/internal/protocol"
)

type Conn struct {
	raw     net.Conn
	writeMu sync.Mutex
}

func New(raw net.Conn) *Conn { return &Conn{raw: raw} }

func (c *Conn) SendFrame(f protocol.Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return protocol.WriteFrame(c.raw, f)
}

func (c *Conn) RecvFrame() (protocol.Frame, error) {
	return protocol.ReadFrame(c.raw)
}

func (c *Conn) Close() error { return c.raw.Close() }

func (c *Conn) SetDeadline(t time.Time) error { return c.raw.SetDeadline(t) }

func (c *Conn) RemoteAddr() net.Addr { return c.raw.RemoteAddr() }
