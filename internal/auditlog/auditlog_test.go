package auditlog

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	cases := []struct{ in, want string }{
		{`copy a b`, `copy a b`},
		{`app.exe password=hunter2 --x`, `app.exe password=*** --x`},
		{`app.exe --password hunter2`, `app.exe --password ***`},
		{`app.exe /pwd:"two words"`, `app.exe /pwd:***`},
		{`net user bob S3cret! /add`, `net user bob *** /add`},
		{`curl https://u:p4ss@host/x`, `curl https://u:***@host/x`},
		{`app.exe -p 5959`, `app.exe -p 5959`},
		{`dir C:\Users\passport`, `dir C:\Users\passport`},
	}
	for _, c := range cases {
		if got := Redact(c.in); got != c.want {
			t.Errorf("Redact(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRotate(t *testing.T) {
	dir, _ := ioutil.TempDir("", "audit")
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "audit.log")
	l, err := OpenWith(p, Options{MaxSize: 300, Keep: 2, Redact: true})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		l.Log("1.2.3.4:5", uint32(i), "exec", "echo password=hunter2")
	}
	l.Close()
	for _, name := range []string{"audit.log", "audit.log.1", "audit.log.2"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s missing: %v", name, err)
		}
	}
	if _, err := os.Stat(p + ".3"); err == nil {
		t.Error("audit.log.3 should have been dropped (Keep=2)")
	}
	b, _ := ioutil.ReadFile(p)
	if strings.Contains(string(b), "hunter2") {
		t.Error("password leaked into the log")
	}
}
