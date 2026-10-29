package session

import "testing"

func TestParseCdCommand(t *testing.T) {
	cases := []struct {
		in   string
		arg  string
		isCd bool
	}{
		{`cd`, "", true},
		{`cd C:\x`, `C:\x`, true},
		{`cd /d D:\work`, `D:\work`, true},
		{`cd "C:\Program Files & Co"`, `"C:\Program Files & Co"`, true},
		{`cd C:\x && prog.exe`, "", false},
		{`cd /d C:\x & dir`, "", false},
		{`cd C:\x | more`, "", false},
		{`cd C:\x > out.txt`, "", false},
		{`dir`, "", false},
	}
	for _, c := range cases {
		arg, ok := parseCdCommand(c.in)
		if ok != c.isCd || arg != c.arg {
			t.Errorf("parseCdCommand(%q) = (%q, %v), want (%q, %v)", c.in, arg, ok, c.arg, c.isCd)
		}
	}
}
