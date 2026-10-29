//go:build !windows
// +build !windows

// Package fileacl restricts files and directories to the account that owns them (plus administrators
// on Windows). Go's 0600 file mode is ignored on Windows, where a file inherits the ACL of its parent.
package fileacl

import "os"

// RestrictFile makes path readable and writable by its owner only.
func RestrictFile(path string) error { return os.Chmod(path, 0600) }

// RestrictDir makes path (and, through inheritance on Windows, what is created in it) owner-only.
func RestrictDir(path string) error { return os.Chmod(path, 0700) }
