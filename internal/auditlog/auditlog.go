package auditlog

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

type Entry struct {
	Time     string `json:"time"`
	Remote   string `json:"remote,omitempty"`
	StreamID uint32 `json:"stream_id"`
	Verb     string `json:"verb"`
	Detail   string `json:"detail,omitempty"`
}

type Logger struct {
	mu sync.Mutex
	f  *os.File
}

func Open(path string) (*Logger, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	return &Logger{f: f}, nil
}

func (l *Logger) Log(remote string, streamID uint32, verb, detail string) {
	if l == nil {
		return
	}
	e := Entry{
		Time:     time.Now().UTC().Format(time.RFC3339),
		Remote:   remote,
		StreamID: streamID,
		Verb:     verb,
		Detail:   detail,
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	b = append(b, '\n')

	l.mu.Lock()
	l.f.Write(b)
	l.mu.Unlock()
}

func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	return l.f.Close()
}
