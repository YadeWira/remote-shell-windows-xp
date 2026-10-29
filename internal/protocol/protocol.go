package protocol

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
)

const Version = 1

const ChunkSize = 64 * 1024

const MaxFrameLen = 16 * 1024 * 1024

const (
	MsgHello       byte = 0x01
	MsgHelloOK     byte = 0x02
	MsgHelloReject byte = 0x03
	MsgPing        byte = 0x04
	MsgPong        byte = 0x05
	MsgStreamEnd   byte = 0x06

	MsgExecStart  byte = 0x10
	MsgExecStdout byte = 0x11
	MsgExecStderr byte = 0x12
	MsgExecStdin  byte = 0x13
	MsgExecExit   byte = 0x14
	MsgExecKill   byte = 0x15

	MsgFSList       byte = 0x20
	MsgFSListResult byte = 0x21
	MsgFSStat       byte = 0x22
	MsgFSStatResult byte = 0x23
	MsgFSMkdir      byte = 0x24
	MsgFSDelete     byte = 0x25
	MsgFSMove       byte = 0x26
	MsgFSCopy       byte = 0x27
	MsgOK           byte = 0x28
	MsgFSTree       byte = 0x29
	MsgFSTreeResult byte = 0x2A

	MsgFSReadStart byte = 0x30
	MsgFSReadChunk byte = 0x31
	MsgFSReadDone  byte = 0x32

	MsgFSWriteStart  byte = 0x33
	MsgFSWriteAck    byte = 0x34
	MsgFSWriteChunk  byte = 0x35
	MsgFSWriteDone   byte = 0x36
	MsgFSWriteResult byte = 0x37

	MsgSysInfo        byte = 0x40
	MsgSysInfoResult  byte = 0x41
	MsgProcList       byte = 0x42
	MsgProcListResult byte = 0x43
	MsgProcKill       byte = 0x44

	MsgError byte = 0xFE
	MsgBye   byte = 0xFF
)

var ErrFrameTooLarge = errors.New("protocol: frame too large")

type Frame struct {
	Type     byte
	StreamID uint32
	Payload  []byte
}

const headerLen = 9

func WriteFrame(w io.Writer, f Frame) error {
	if len(f.Payload) > MaxFrameLen {
		return ErrFrameTooLarge
	}
	header := encodeHeader(f)
	if _, err := w.Write(header); err != nil {
		return err
	}
	if len(f.Payload) > 0 {
		if _, err := w.Write(f.Payload); err != nil {
			return err
		}
	}
	return nil
}

func ReadFrame(r io.Reader) (Frame, error) {
	header := make([]byte, headerLen)
	if _, err := io.ReadFull(r, header); err != nil {
		return Frame{}, err
	}
	f, length, err := decodeHeader(header)
	if err != nil {
		return Frame{}, err
	}
	if length > 0 {
		f.Payload = make([]byte, length)
		if _, err := io.ReadFull(r, f.Payload); err != nil {
			return Frame{}, err
		}
	}
	return f, nil
}

func encodeHeader(f Frame) []byte {
	header := make([]byte, headerLen)
	binary.BigEndian.PutUint32(header[0:4], uint32(len(f.Payload)))
	header[4] = f.Type
	binary.BigEndian.PutUint32(header[5:9], f.StreamID)
	return header
}

func decodeHeader(header []byte) (Frame, uint32, error) {
	length := binary.BigEndian.Uint32(header[0:4])
	if length > MaxFrameLen {
		return Frame{}, 0, ErrFrameTooLarge
	}
	f := Frame{
		Type:     header[4],
		StreamID: binary.BigEndian.Uint32(header[5:9]),
	}
	return f, length, nil
}

func Marshal(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic("protocol: marshal: " + err.Error())
	}
	return b
}

func Unmarshal(b []byte, v interface{}) error {
	return json.Unmarshal(b, v)
}

func EncodeChunk(offset int64, data []byte) []byte {
	out := make([]byte, 8+len(data))
	binary.BigEndian.PutUint64(out[0:8], uint64(offset))
	copy(out[8:], data)
	return out
}

func DecodeChunk(payload []byte) (offset int64, data []byte, err error) {
	if len(payload) < 8 {
		return 0, nil, errors.New("protocol: chunk payload too short")
	}
	offset = int64(binary.BigEndian.Uint64(payload[0:8]))
	return offset, payload[8:], nil
}

type HelloRequest struct {
	Version int    `json:"version"`
	Token   string `json:"token"`
	// Name optionally identifies the controller (user@host by default in xpctl); the agent
	// records it in the audit log next to the address.
	Name string `json:"name,omitempty"`
}

type HelloResponse struct {
	OK       bool   `json:"ok"`
	Message  string `json:"message,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}

type ExecStartRequest struct {
	Command     string `json:"command"`
	Cwd         string `json:"cwd,omitempty"`
	Interactive bool   `json:"interactive"`
	// Desktop starts the command on the interactive desktop (WinSta0\Default) of a Windows agent,
	// so its windows are visible. Output is not captured. Without Wait the call returns at once
	// with the process id.
	Desktop bool `json:"desktop,omitempty"`
	// Wait (with Desktop) waits for the process to exit and reports its exit code.
	Wait bool `json:"wait,omitempty"`
}

type ExecExitResult struct {
	ExitCode int    `json:"exit_code"`
	Cwd      string `json:"cwd"`
	Error    string `json:"error,omitempty"`
}

type FSListRequest struct {
	Path string `json:"path"`
}

type FSEntry struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}

type FSListResult struct {
	Entries []FSEntry `json:"entries"`
}

type FSStatRequest struct {
	Path string `json:"path"`
}

type FSStatResult struct {
	Exists  bool   `json:"exists"`
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}

type FSMkdirRequest struct {
	Path string `json:"path"`
}

type FSDeleteRequest struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
}

type FSMoveRequest struct {
	Src string `json:"src"`
	Dst string `json:"dst"`
}

type FSCopyRequest struct {
	Src string `json:"src"`
	Dst string `json:"dst"`
}

type Result struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

type FSTreeRequest struct {
	Path     string `json:"path"`
	MaxDepth int    `json:"max_depth"`
}

type FSTreeEntry struct {
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

type FSTreeResult struct {
	Entries   []FSTreeEntry `json:"entries"`
	Truncated bool          `json:"truncated"`
}

type FSReadStartRequest struct {
	Path string `json:"path"`
}

type FSReadDoneResult struct {
	Sha256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type FSWriteStartRequest struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Overwrite bool   `json:"overwrite"`
	Resume    bool   `json:"resume"`
}

type FSWriteAckResult struct {
	Offset int64 `json:"offset"`
}

type FSWriteDoneRequest struct {
	Sha256 string `json:"sha256"`
}

type SysInfoResult struct {
	Hostname   string      `json:"hostname"`
	OSVersion  string      `json:"os_version"`
	Arch       string      `json:"arch"`
	UptimeSec  int64       `json:"uptime_sec"`
	TotalMemMB int64       `json:"total_mem_mb"`
	FreeMemMB  int64       `json:"free_mem_mb"`
	Drives     []DriveInfo `json:"drives"`
}

type DriveInfo struct {
	Letter  string `json:"letter"`
	TotalMB int64  `json:"total_mb"`
	FreeMB  int64  `json:"free_mb"`
}

type ProcInfo struct {
	PID  uint32 `json:"pid"`
	PPID uint32 `json:"ppid"`
	Name string `json:"name"`
}

type ProcListResult struct {
	Processes []ProcInfo `json:"processes"`
}

type ProcKillRequest struct {
	PID uint32 `json:"pid"`
}

type ErrorPayload struct {
	Message string `json:"message"`
}
