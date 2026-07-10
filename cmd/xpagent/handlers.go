package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/BaseMax/remote-shell-windows-xp/internal/fsops"
	"github.com/BaseMax/remote-shell-windows-xp/internal/procops"
	"github.com/BaseMax/remote-shell-windows-xp/internal/protocol"
	"github.com/BaseMax/remote-shell-windows-xp/internal/session"
	"github.com/BaseMax/remote-shell-windows-xp/internal/sysinfo"
	"github.com/BaseMax/remote-shell-windows-xp/internal/transport"
)

func (a *agent) handleConn(raw net.Conn) {
	defer raw.Close()
	remote := raw.RemoteAddr().String()

	conn := transport.New(raw)
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	first, err := conn.RecvFrame()
	if err != nil {
		return
	}
	if err := transport.ServerHandshake(conn, first, a.token, a.hostname); err != nil {
		a.audit.Log(remote, 0, "auth-failed", err.Error())
		return
	}
	conn.SetDeadline(time.Time{})
	a.audit.Log(remote, 0, "connect", "")

	sm := session.NewManager()
	mux := transport.NewMultiplexer(conn)
	go mux.Run()

	a.pumpConnection(mux, sm, remote)

	a.audit.Log(remote, 0, "disconnect", "")
}

func (a *agent) pumpConnection(mux *transport.Multiplexer, sm *session.Manager, remote string) {
	for {
		select {
		case ns, ok := <-mux.NewStreams():
			if !ok {
				return
			}
			go a.handleStream(mux, sm, ns, remote)
		case cf, ok := <-mux.Control():
			if !ok {
				return
			}
			if cf.Type == protocol.MsgPing {
				mux.Send(protocol.Frame{Type: protocol.MsgPong})
			}
		case <-mux.Done():
			return
		}
	}
}

func (a *agent) handleStream(mux *transport.Multiplexer, sm *session.Manager, ns transport.NewStream, remote string) {
	id := ns.ID
	defer func() {
		mux.CloseStream(id)
		sm.Forget(id)
	}()

	switch ns.First.Type {
	case protocol.MsgExecStart:
		a.handleExec(mux, sm, id, ns.Frames, ns.First, remote)
	case protocol.MsgFSList:
		a.respondFSList(mux, id, ns.First, remote)
	case protocol.MsgFSStat:
		a.respondFSStat(mux, id, ns.First, remote)
	case protocol.MsgFSMkdir:
		a.respondFSMkdir(mux, id, ns.First, remote)
	case protocol.MsgFSDelete:
		a.respondFSDelete(mux, id, ns.First, remote)
	case protocol.MsgFSMove:
		a.respondFSMove(mux, id, ns.First, remote)
	case protocol.MsgFSCopy:
		a.respondFSCopy(mux, id, ns.First, remote)
	case protocol.MsgFSTree:
		a.respondFSTree(mux, id, ns.First, remote)
	case protocol.MsgFSReadStart:
		a.handleFSRead(mux, id, ns.Frames, ns.First, remote)
	case protocol.MsgFSWriteStart:
		a.handleFSWrite(mux, id, ns.Frames, ns.First, remote)
	case protocol.MsgSysInfo:
		a.respondSysInfo(mux, id, remote)
	case protocol.MsgProcList:
		a.respondProcList(mux, id, remote)
	case protocol.MsgProcKill:
		a.respondProcKill(mux, id, ns.First, remote)
	default:
		mux.Send(errFrame(id, fmt.Errorf("unknown message type 0x%02x", ns.First.Type)))
	}
}

func (a *agent) handleExec(mux *transport.Multiplexer, sm *session.Manager, id uint32, frames <-chan protocol.Frame, first protocol.Frame, remote string) {
	f := first
	for {
		if f.Type == protocol.MsgStreamEnd {
			return
		}
		if f.Type != protocol.MsgExecStart {
			nf, ok := <-frames
			if !ok {
				return
			}
			f = nf
			continue
		}

		if !a.runExecOnce(mux, sm, id, frames, f, remote) {
			return
		}

		nf, ok := <-frames
		if !ok {
			return
		}
		f = nf
	}
}

func (a *agent) runExecOnce(mux *transport.Multiplexer, sm *session.Manager, id uint32, frames <-chan protocol.Frame, f protocol.Frame, remote string) bool {
	var req protocol.ExecStartRequest
	protocol.Unmarshal(f.Payload, &req)
	a.audit.Log(remote, id, "exec", req.Command)

	stdin, killCh, resultCh := sm.StartExec(id, req,
		func(b []byte) { mux.Send(protocol.Frame{Type: protocol.MsgExecStdout, StreamID: id, Payload: b}) },
		func(b []byte) { mux.Send(protocol.Frame{Type: protocol.MsgExecStderr, StreamID: id, Payload: b}) },
	)

	return waitForExit(mux, id, frames, stdin, killCh, resultCh)
}

func waitForExit(mux *transport.Multiplexer, id uint32, frames <-chan protocol.Frame, stdin io.WriteCloser, killCh chan<- struct{}, resultCh <-chan session.Result) bool {
	killed := false
	requestKill := func() {
		if !killed {
			killed = true
			close(killCh)
		}
	}

	for {
		select {
		case res := <-resultCh:
			mux.Send(protocol.Frame{Type: protocol.MsgExecExit, StreamID: id, Payload: protocol.Marshal(protocol.ExecExitResult{ExitCode: res.ExitCode, Cwd: res.Cwd})})
			return true
		case nf, ok := <-frames:
			if !ok {
				if stdin != nil {
					stdin.Close()
				}
				requestKill()
				return false
			}
			switch nf.Type {
			case protocol.MsgExecStdin:
				if stdin != nil {
					stdin.Write(nf.Payload)
				}
			case protocol.MsgExecKill:
				requestKill()
			case protocol.MsgStreamEnd:
				requestKill()
				return false
			}
		}
	}
}

func respond(mux *transport.Multiplexer, id uint32, resultType byte, result interface{}, err error) {
	if err != nil {
		mux.Send(errFrame(id, err))
		return
	}
	mux.Send(protocol.Frame{Type: resultType, StreamID: id, Payload: protocol.Marshal(result)})
}

func respondOK(mux *transport.Multiplexer, id uint32, err error) {
	if err != nil {
		mux.Send(errFrame(id, err))
		return
	}
	mux.Send(protocol.Frame{Type: protocol.MsgOK, StreamID: id, Payload: protocol.Marshal(protocol.Result{OK: true})})
}

func (a *agent) respondFSList(mux *transport.Multiplexer, id uint32, first protocol.Frame, remote string) {
	var req protocol.FSListRequest
	protocol.Unmarshal(first.Payload, &req)
	a.audit.Log(remote, id, "ls", req.Path)
	entries, err := fsops.List(req.Path)
	respond(mux, id, protocol.MsgFSListResult, protocol.FSListResult{Entries: entries}, err)
}

func (a *agent) respondFSStat(mux *transport.Multiplexer, id uint32, first protocol.Frame, remote string) {
	var req protocol.FSStatRequest
	protocol.Unmarshal(first.Payload, &req)
	a.audit.Log(remote, id, "stat", req.Path)
	res, err := fsops.Stat(req.Path)
	respond(mux, id, protocol.MsgFSStatResult, res, err)
}

func (a *agent) respondFSMkdir(mux *transport.Multiplexer, id uint32, first protocol.Frame, remote string) {
	var req protocol.FSMkdirRequest
	protocol.Unmarshal(first.Payload, &req)
	a.audit.Log(remote, id, "mkdir", req.Path)
	respondOK(mux, id, fsops.Mkdir(req.Path))
}

func (a *agent) respondFSDelete(mux *transport.Multiplexer, id uint32, first protocol.Frame, remote string) {
	var req protocol.FSDeleteRequest
	protocol.Unmarshal(first.Payload, &req)
	a.audit.Log(remote, id, "rm", req.Path)
	respondOK(mux, id, fsops.Delete(req.Path, req.Recursive))
}

func (a *agent) respondFSMove(mux *transport.Multiplexer, id uint32, first protocol.Frame, remote string) {
	var req protocol.FSMoveRequest
	protocol.Unmarshal(first.Payload, &req)
	a.audit.Log(remote, id, "mv", req.Src+" -> "+req.Dst)
	respondOK(mux, id, fsops.Move(req.Src, req.Dst))
}

func (a *agent) respondFSCopy(mux *transport.Multiplexer, id uint32, first protocol.Frame, remote string) {
	var req protocol.FSCopyRequest
	protocol.Unmarshal(first.Payload, &req)
	a.audit.Log(remote, id, "cp", req.Src+" -> "+req.Dst)
	respondOK(mux, id, fsops.Copy(req.Src, req.Dst))
}

func (a *agent) respondFSTree(mux *transport.Multiplexer, id uint32, first protocol.Frame, remote string) {
	var req protocol.FSTreeRequest
	protocol.Unmarshal(first.Payload, &req)
	a.audit.Log(remote, id, "tree", req.Path)
	entries, truncated, err := fsops.Tree(req.Path, req.MaxDepth)
	respond(mux, id, protocol.MsgFSTreeResult, protocol.FSTreeResult{Entries: entries, Truncated: truncated}, err)
}

func (a *agent) handleFSRead(mux *transport.Multiplexer, id uint32, frames <-chan protocol.Frame, first protocol.Frame, remote string) {
	var req protocol.FSReadStartRequest
	protocol.Unmarshal(first.Payload, &req)
	a.audit.Log(remote, id, "get", req.Path)

	f, err := os.Open(req.Path)
	if err != nil {
		mux.Send(errFrame(id, err))
		return
	}
	defer f.Close()

	sum, total, err := streamFileToClient(mux, id, f)
	if err != nil {
		return
	}
	mux.Send(protocol.Frame{Type: protocol.MsgFSReadDone, StreamID: id, Payload: protocol.Marshal(protocol.FSReadDoneResult{
		Sha256: sum,
		Size:   total,
	})})

	for nf := range frames {
		if nf.Type == protocol.MsgStreamEnd {
			return
		}
	}
}

func streamFileToClient(mux *transport.Multiplexer, id uint32, f *os.File) (sha256Hex string, total int64, err error) {
	hasher := sha256.New()
	buf := make([]byte, protocol.ChunkSize)
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			hasher.Write(chunk)
			total += int64(n)
			if serr := mux.Send(protocol.Frame{Type: protocol.MsgFSReadChunk, StreamID: id, Payload: chunk}); serr != nil {
				return "", 0, serr
			}
		}
		if rerr != nil {
			break
		}
	}
	return hex.EncodeToString(hasher.Sum(nil)), total, nil
}

func (a *agent) handleFSWrite(mux *transport.Multiplexer, id uint32, frames <-chan protocol.Frame, first protocol.Frame, remote string) {
	var req protocol.FSWriteStartRequest
	protocol.Unmarshal(first.Payload, &req)
	a.audit.Log(remote, id, "put", req.Path)

	up, offset, err := fsops.NewUpload(req.Path, req.Resume)
	if err != nil {
		mux.Send(errFrame(id, err))
		return
	}
	defer up.Abort()

	if serr := mux.Send(protocol.Frame{Type: protocol.MsgFSWriteAck, StreamID: id, Payload: protocol.Marshal(protocol.FSWriteAckResult{Offset: offset})}); serr != nil {
		return
	}

	for nf := range frames {
		switch nf.Type {
		case protocol.MsgFSWriteChunk:
			off, data, derr := protocol.DecodeChunk(nf.Payload)
			if derr != nil {
				continue
			}
			if werr := up.WriteChunk(off, data); werr != nil {
				mux.Send(errFrame(id, werr))
				return
			}
		case protocol.MsgFSWriteDone:
			a.finishUpload(mux, id, up, req.Overwrite, nf)
			return
		case protocol.MsgStreamEnd:
			return
		}
	}
}

func (a *agent) finishUpload(mux *transport.Multiplexer, id uint32, up *fsops.Upload, overwrite bool, nf protocol.Frame) {
	var done protocol.FSWriteDoneRequest
	protocol.Unmarshal(nf.Payload, &done)
	if err := up.Finish(done.Sha256, overwrite); err != nil {
		mux.Send(protocol.Frame{Type: protocol.MsgFSWriteResult, StreamID: id, Payload: protocol.Marshal(protocol.Result{OK: false, Message: err.Error()})})
		return
	}
	mux.Send(protocol.Frame{Type: protocol.MsgFSWriteResult, StreamID: id, Payload: protocol.Marshal(protocol.Result{OK: true})})
}

func (a *agent) respondSysInfo(mux *transport.Multiplexer, id uint32, remote string) {
	a.audit.Log(remote, id, "sysinfo", "")
	info, err := sysinfo.Collect()
	respond(mux, id, protocol.MsgSysInfoResult, info, err)
}

func (a *agent) respondProcList(mux *transport.Multiplexer, id uint32, remote string) {
	a.audit.Log(remote, id, "ps", "")
	procs, err := procops.List()
	respond(mux, id, protocol.MsgProcListResult, protocol.ProcListResult{Processes: procs}, err)
}

func (a *agent) respondProcKill(mux *transport.Multiplexer, id uint32, first protocol.Frame, remote string) {
	var req protocol.ProcKillRequest
	protocol.Unmarshal(first.Payload, &req)
	a.audit.Log(remote, id, "kill", strconv.FormatUint(uint64(req.PID), 10))
	respondOK(mux, id, procops.Kill(req.PID))
}

func errFrame(id uint32, err error) protocol.Frame {
	return protocol.Frame{Type: protocol.MsgError, StreamID: id, Payload: protocol.Marshal(protocol.ErrorPayload{Message: err.Error()})}
}
