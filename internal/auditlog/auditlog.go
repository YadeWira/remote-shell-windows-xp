package auditlog

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
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

// Options tune the log. The zero value logs everything, never rotates and does not redact.
type Options struct {
	// MaxSize rotates the log (audit.log -> audit.log.1 -> ...) when it grows past this many bytes; 0 = never.
	MaxSize int64
	// Keep is how many rotated files are kept (default 3).
	Keep int
	// Redact hides values that look like passwords or tokens in command lines (best effort).
	Redact bool
	// OnError, if set, is told about write and rotation failures (otherwise they are silent).
	OnError func(error)
}

type Logger struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
	opts Options
}

// Open opens the log with default options (no rotation, no redaction).
func Open(path string) (*Logger, error) { return OpenWith(path, Options{}) }

func OpenWith(path string, opts Options) (*Logger, error) {
	if opts.Keep <= 0 {
		opts.Keep = 3
	}
	l := &Logger{path: path, opts: opts}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Logger) open() error {
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	l.f = f
	l.size = 0
	if st, err := f.Stat(); err == nil {
		l.size = st.Size()
	}
	return nil
}

// Path returns the file the log writes to.
func (l *Logger) Path() string { return l.path }

func (l *Logger) Log(remote string, streamID uint32, verb, detail string) {
	if l == nil {
		return
	}
	if l.opts.Redact {
		detail = Redact(detail)
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
	defer l.mu.Unlock()
	if l.opts.MaxSize > 0 && l.size+int64(len(b)) > l.opts.MaxSize && l.size > 0 {
		l.rotate()
	}
	n, err := l.f.Write(b)
	l.size += int64(n)
	if err != nil {
		l.report(err)
	}
}

func (l *Logger) report(err error) {
	if l.opts.OnError != nil {
		l.opts.OnError(err)
	}
}

// rotate shifts audit.log.N -> audit.log.N+1 (dropping the oldest), moves the current file to
// audit.log.1 and starts a new one. Called with l.mu held.
func (l *Logger) rotate() {
	l.f.Close()
	os.Remove(fmt.Sprintf("%s.%d", l.path, l.opts.Keep))
	for i := l.opts.Keep - 1; i >= 1; i-- {
		os.Rename(fmt.Sprintf("%s.%d", l.path, i), fmt.Sprintf("%s.%d", l.path, i+1))
	}
	if err := os.Rename(l.path, l.path+".1"); err != nil {
		l.report(err)
	}
	if err := l.open(); err != nil {
		l.report(err)
		// Keep a usable handle even if the new file cannot be created.
		if f, e := os.OpenFile(os.DevNull, os.O_WRONLY, 0); e == nil {
			l.f = f
		}
	}
}

func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	return l.f.Close()
}

var redactors = []struct {
	re   *regexp.Regexp
	repl string
}{
	// password=..., pwd: "..." , token=abc ...
	{regexp.MustCompile(`(?i)\b(pass(?:word|wd)?|pwd|secret|token|api[-_]?key|auth)(\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s&"']+)`), `${1}${2}***`},
	// --password abc, /password:abc, -token abc
	{regexp.MustCompile(`(?i)(^|\s)(--?|/)(pass(?:word|wd)?|pwd|secret|token|api[-_]?key)(\s+|[=:])("[^"]*"|'[^']*'|[^\s"']+)`), `${1}${2}${3}${4}***`},
	// net user NAME PASSWORD
	{regexp.MustCompile(`(?i)\bnet\s+user\s+("[^"]+"|\S+)\s+("[^"]*"|[^\s/]+)`), `net user ${1} ***`},
	// credentials inside URLs: scheme://user:pass@host
	{regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^\s/:@]+):[^\s/@]+@`), `${1}:***@`},
}

// Redact hides the value of password-like options in a command line. It is a best-effort filter
// for the common shapes (key=value, --password x, net user, user:pass@host), not a guarantee.
func Redact(s string) string {
	for _, r := range redactors {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}
