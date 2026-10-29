package transport

import (
	"net"
	"testing"
	"time"

	"github.com/BaseMax/remote-shell-windows-xp/internal/protocol"
)

func muxPair(t *testing.T) (client *Conn, server *Multiplexer) {
	a, b := net.Pipe()
	client = New(a)
	server = NewMultiplexer(New(b))
	go server.Run()
	t.Cleanup(func() { a.Close(); b.Close() })
	return client, server
}

func nextStream(t *testing.T, m *Multiplexer) (NewStream, bool) {
	select {
	case ns := <-m.NewStreams():
		return ns, true
	case <-time.After(500 * time.Millisecond):
		return NewStream{}, false
	}
}

// A late frame for a stream that already ended must not open a new stream.
func TestLateFrameDoesNotReopenStream(t *testing.T) {
	client, server := muxPair(t)

	client.SendFrame(protocol.Frame{Type: protocol.MsgSysInfo, StreamID: 1})
	ns, ok := nextStream(t, server)
	if !ok || ns.ID != 1 {
		t.Fatalf("expected stream 1, got %v %v", ns.ID, ok)
	}
	server.CloseStream(1)

	client.SendFrame(protocol.Frame{Type: protocol.MsgStreamEnd, StreamID: 1}) // late
	client.SendFrame(protocol.Frame{Type: protocol.MsgSysInfo, StreamID: 2})
	ns, ok = nextStream(t, server)
	if !ok || ns.ID != 2 {
		t.Fatalf("expected stream 2 next (the late frame for 1 must be dropped), got id=%d ok=%v", ns.ID, ok)
	}
}

func TestClosedMemoryIsBounded(t *testing.T) {
	m := NewMultiplexer(nil)
	for i := uint32(1); i <= closedMemory+500; i++ {
		m.CloseStream(i)
	}
	if len(m.closedIDs) != closedMemory || len(m.closedRing) != closedMemory {
		t.Fatalf("closed ids kept: map=%d ring=%d, want %d", len(m.closedIDs), len(m.closedRing), closedMemory)
	}
	if _, ok := m.closedIDs[1]; ok {
		t.Error("oldest id should have been forgotten")
	}
	if _, ok := m.closedIDs[closedMemory+500]; !ok {
		t.Error("newest id should be remembered")
	}
}
