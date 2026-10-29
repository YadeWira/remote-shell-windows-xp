package transport

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BaseMax/remote-shell-windows-xp/internal/protocol"
)

var ErrClosed = errors.New("transport: multiplexer closed")

const (
	// closedMemory is how many recently closed stream ids are remembered, so a late frame for
	// one of them is dropped instead of opening a new stream.
	closedMemory = 4096
	// streamStallLimit is how long a full stream queue may hold up the connection before that
	// one stream is dropped. Waiting a while keeps the TCP back-pressure that large uploads rely on.
	streamStallLimit = 10 * time.Second
)

type NewStream struct {
	ID     uint32
	Frames <-chan protocol.Frame
	First  protocol.Frame
}

type Multiplexer struct {
	conn *Conn

	mu         sync.Mutex
	streams    map[uint32]chan protocol.Frame
	closedIDs  map[uint32]struct{}
	closedRing []uint32
	closedNext int

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
		closedIDs:  make(map[uint32]struct{}),
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
	m.closeStreamLocked(id)
	m.mu.Unlock()
}

// closeStreamLocked drops the stream and remembers its id (bounded) so late frames are ignored.
func (m *Multiplexer) closeStreamLocked(id uint32) {
	if ch, ok := m.streams[id]; ok {
		delete(m.streams, id)
		close(ch)
	}
	if _, seen := m.closedIDs[id]; seen {
		return
	}
	if len(m.closedRing) < closedMemory {
		m.closedRing = append(m.closedRing, id)
	} else {
		delete(m.closedIDs, m.closedRing[m.closedNext])
		m.closedRing[m.closedNext] = id
		m.closedNext = (m.closedNext + 1) % closedMemory
	}
	m.closedIDs[id] = struct{}{}
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
	if _, closed := m.closedIDs[f.StreamID]; closed {
		m.mu.Unlock()
		return nil // late frame for a stream that already ended
	}
	ch, ok := m.streams[f.StreamID]
	if !ok {
		newCh := make(chan protocol.Frame, 64)
		m.streams[f.StreamID] = newCh
		m.mu.Unlock()
		return m.deliverNewStream(NewStream{ID: f.StreamID, Frames: newCh, First: f})
	}
	m.mu.Unlock()

	return m.deliverStream(f.StreamID, ch, f)
}

// deliverStream queues the frame for a stream. If the stream's queue stays full for
// streamStallLimit (its handler is not reading), only that stream is dropped, with an error
// frame to the peer, instead of blocking every other stream on the connection forever.
func (m *Multiplexer) deliverStream(id uint32, ch chan protocol.Frame, f protocol.Frame) (err error) {
	// A handler may close the stream (closing ch) while the read loop is about to queue a frame.
	defer func() {
		if recover() != nil {
			err = nil
		}
	}()
	select {
	case ch <- f:
		return nil
	case <-m.closed:
		return ErrClosed
	default:
	}
	timer := time.NewTimer(streamStallLimit)
	defer timer.Stop()
	select {
	case ch <- f:
		return nil
	case <-m.closed:
		return ErrClosed
	case <-timer.C:
		m.mu.Lock()
		m.closeStreamLocked(id)
		m.mu.Unlock()
		m.conn.SendFrame(protocol.Frame{Type: protocol.MsgError, StreamID: id,
			Payload: protocol.Marshal(protocol.ErrorPayload{Message: "stream backlog full: dropped"})})
		return nil
	}
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
