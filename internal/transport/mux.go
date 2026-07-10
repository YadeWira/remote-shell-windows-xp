package transport

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/BaseMax/remote-shell-windows-xp/internal/protocol"
)

var ErrClosed = errors.New("transport: multiplexer closed")

type NewStream struct {
	ID     uint32
	Frames <-chan protocol.Frame
	First  protocol.Frame
}

type Multiplexer struct {
	conn *Conn

	mu      sync.Mutex
	streams map[uint32]chan protocol.Frame

	control    chan protocol.Frame
	newStreams chan NewStream
	closed     chan struct{}
	closeOnce  sync.Once
	closeErr   error

	nextID uint32
}

func NewMultiplexer(conn *Conn) *Multiplexer {
	return &Multiplexer{
		conn:       conn,
		streams:    make(map[uint32]chan protocol.Frame),
		control:    make(chan protocol.Frame, 32),
		newStreams: make(chan NewStream, 8),
		closed:     make(chan struct{}),
	}
}

func (m *Multiplexer) NextStreamID() uint32 { return atomic.AddUint32(&m.nextID, 1) }

func (m *Multiplexer) Open(id uint32) <-chan protocol.Frame {
	ch := make(chan protocol.Frame, 64)
	m.mu.Lock()
	m.streams[id] = ch
	m.mu.Unlock()
	return ch
}

func (m *Multiplexer) CloseStream(id uint32) {
	m.mu.Lock()
	if ch, ok := m.streams[id]; ok {
		delete(m.streams, id)
		close(ch)
	}
	m.mu.Unlock()
}

func (m *Multiplexer) Control() <-chan protocol.Frame { return m.control }

func (m *Multiplexer) NewStreams() <-chan NewStream { return m.newStreams }

func (m *Multiplexer) Done() <-chan struct{} { return m.closed }

func (m *Multiplexer) Err() error { return m.closeErr }

func (m *Multiplexer) Send(f protocol.Frame) error { return m.conn.SendFrame(f) }

func (m *Multiplexer) Run() error {
	for {
		f, err := m.conn.RecvFrame()
		if err != nil {
			m.shutdown(err)
			return err
		}
		if err := m.route(f); err != nil {
			return err
		}
	}
}

func (m *Multiplexer) route(f protocol.Frame) error {
	if f.StreamID == 0 {
		return m.deliver(m.control, f)
	}

	m.mu.Lock()
	ch, ok := m.streams[f.StreamID]
	if !ok {
		newCh := make(chan protocol.Frame, 64)
		m.streams[f.StreamID] = newCh
		m.mu.Unlock()
		return m.deliverNewStream(NewStream{ID: f.StreamID, Frames: newCh, First: f})
	}
	m.mu.Unlock()

	return m.deliver(ch, f)
}

func (m *Multiplexer) deliver(ch chan protocol.Frame, f protocol.Frame) error {
	select {
	case ch <- f:
		return nil
	case <-m.closed:
		return ErrClosed
	}
}

func (m *Multiplexer) deliverNewStream(ns NewStream) error {
	select {
	case m.newStreams <- ns:
		return nil
	case <-m.closed:
		return ErrClosed
	}
}

func (m *Multiplexer) shutdown(err error) {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closeErr = err
		for id, ch := range m.streams {
			close(ch)
			delete(m.streams, id)
		}
		m.mu.Unlock()
		close(m.closed)
	})
}

func (m *Multiplexer) Close() error {
	m.shutdown(ErrClosed)
	return m.conn.Close()
}
