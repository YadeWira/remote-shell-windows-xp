package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"

	"github.com/BaseMax/remote-shell-windows-xp/internal/protocol"
)

func cmdTree(args []string) error {
	fs := flag.NewFlagSet("tree", flag.ExitOnError)
	depth := fs.Int("depth", 0, "maximum depth to descend (0 = unlimited)")
	asJSON := fs.Bool("json", false, "print machine-readable JSON instead of an ASCII tree")
	fs.Parse(reorderArgs(fs, args))
	rest := fs.Args()
	if len(rest) < 2 {
		return fmt.Errorf("usage: xpctl tree <addr:port> <path> [--depth N] [--json]")
	}
	addr, path := rest[0], rest[1]

	var res protocol.FSTreeResult
	req := protocol.FSTreeRequest{Path: path, MaxDepth: *depth}
	if err := simpleRequest(addr, protocol.MsgFSTree, protocol.Marshal(req), &res); err != nil {
		return err
	}

	if *asJSON {
		out, _ := json.Marshal(res)
		fmt.Println(string(out))
		return nil
	}

	printTreeResult(path, res)
	return nil
}

type treeNode struct {
	name     string
	isDir    bool
	children []*treeNode
}

func printTreeResult(root string, res protocol.FSTreeResult) {
	fmt.Println(root)

	index := map[string]*treeNode{"": {isDir: true}}
	for _, e := range res.Entries {
		parent := index[parentPath(e.Path)]
		if parent == nil {
			parent = index[""]
		}
		node := &treeNode{name: baseName(e.Path), isDir: e.IsDir}
		parent.children = append(parent.children, node)
		index[e.Path] = node
	}

	dirs, files := printTreeChildren(index[""], "")

	fmt.Printf("\n%d %s, %d %s\n", dirs, pluralize(dirs, "directory", "directories"), files, pluralize(files, "file", "files"))
	if res.Truncated {
		fmt.Println("note: output truncated, the tree is larger than what was returned")
	}
}

func printTreeChildren(node *treeNode, prefix string) (dirs, files int) {
	for i, child := range node.children {
		last := i == len(node.children)-1
		connector, nextPrefix := "├── ", prefix+"│   "
		if last {
			connector, nextPrefix = "└── ", prefix+"    "
		}

		label := child.name
		if child.isDir {
			label += "/"
			dirs++
		} else {
			files++
		}
		fmt.Println(prefix + connector + label)

		if child.isDir {
			childDirs, childFiles := printTreeChildren(child, nextPrefix)
			dirs += childDirs
			files += childFiles
		}
	}
	return dirs, files
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

func parentPath(p string) string {
	idx := strings.LastIndexByte(p, '/')
	if idx < 0 {
		return ""
	}
	return p[:idx]
}

func baseName(p string) string {
	idx := strings.LastIndexByte(p, '/')
	if idx < 0 {
		return p
	}
	return p[idx+1:]
}
