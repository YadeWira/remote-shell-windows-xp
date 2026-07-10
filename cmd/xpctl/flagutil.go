package main

import (
	"flag"
	"strings"
)

func reorderArgs(fs *flag.FlagSet, args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) < 2 || a[0] != '-' {
			positional = append(positional, a)
			continue
		}

		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if strings.ContainsRune(name, '=') {
			continue
		}
		if takesValue(fs, name) && i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	return append(flags, positional...)
}

func takesValue(fs *flag.FlagSet, name string) bool {
	fl := fs.Lookup(name)
	if fl == nil {
		return true
	}
	bf, ok := fl.Value.(interface{ IsBoolFlag() bool })
	return !ok || !bf.IsBoolFlag()
}
