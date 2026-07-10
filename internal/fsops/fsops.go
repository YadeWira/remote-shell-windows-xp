package fsops

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/BaseMax/remote-shell-windows-xp/internal/protocol"
)

const maxTreeEntries = 50000

func List(path string) ([]protocol.FSEntry, error) {
	entries, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.FSEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, protocol.FSEntry{
			Name:    e.Name(),
			IsDir:   e.IsDir(),
			Size:    e.Size(),
			ModTime: e.ModTime().UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

func Tree(root string, maxDepth int) ([]protocol.FSTreeEntry, bool, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, false, err
	}
	if !info.IsDir() {
		return nil, false, fmt.Errorf("fsops: %s is not a directory", root)
	}

	var entries []protocol.FSTreeEntry
	truncated := false

	var walk func(dir, relPrefix string, depth int)
	walk = func(dir, relPrefix string, depth int) {
		children, err := ioutil.ReadDir(dir)
		if err != nil {
			return
		}
		for _, c := range children {
			if len(entries) >= maxTreeEntries {
				truncated = true
				return
			}
			relPath := path.Join(relPrefix, c.Name())
			entries = append(entries, protocol.FSTreeEntry{Path: relPath, IsDir: c.IsDir(), Size: c.Size()})
			if c.IsDir() && (maxDepth <= 0 || depth < maxDepth) {
				walk(filepath.Join(dir, c.Name()), relPath, depth+1)
			}
		}
	}

	walk(root, "", 1)
	return entries, truncated, nil
}

func Stat(path string) (protocol.FSStatResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return protocol.FSStatResult{Exists: false}, nil
		}
		return protocol.FSStatResult{}, err
	}
	return protocol.FSStatResult{
		Exists:  true,
		IsDir:   info.IsDir(),
		Size:    info.Size(),
		ModTime: info.ModTime().UTC().Format(time.RFC3339),
	}, nil
}

func Mkdir(path string) error { return os.MkdirAll(path, 0755) }

func Delete(path string, recursive bool) error {
	if recursive {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

func Move(src, dst string) error { return os.Rename(src, dst) }

func Copy(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst, info.Mode())
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	entries, err := ioutil.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDir(s, d); err != nil {
				return err
			}
		} else if err := copyFile(s, d, e.Mode()); err != nil {
			return err
		}
	}
	return nil
}

func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type Upload struct {
	tmpPath   string
	finalPath string
	f         *os.File
}

func NewUpload(path string, resume bool) (*Upload, int64, error) {
	tmp := path + ".xpupload.tmp"
	var startOffset int64
	flag := os.O_CREATE | os.O_WRONLY
	if resume {
		if info, err := os.Stat(tmp); err == nil {
			startOffset = info.Size()
		}
	} else {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(tmp, flag, 0644)
	if err != nil {
		return nil, 0, err
	}
	if startOffset > 0 {
		if _, err := f.Seek(startOffset, io.SeekStart); err != nil {
			f.Close()
			return nil, 0, err
		}
	}
	return &Upload{tmpPath: tmp, finalPath: path, f: f}, startOffset, nil
}

func (u *Upload) WriteChunk(offset int64, data []byte) error {
	_, err := u.f.WriteAt(data, offset)
	return err
}

func (u *Upload) Finish(expectedSha256 string, overwrite bool) error {
	if err := u.f.Close(); err != nil {
		return err
	}
	if expectedSha256 != "" {
		sum, err := FileSHA256(u.tmpPath)
		if err != nil {
			return err
		}
		if sum != expectedSha256 {
			return fmt.Errorf("fsops: checksum mismatch: expected %s got %s", expectedSha256, sum)
		}
	}
	if !overwrite {
		if _, err := os.Stat(u.finalPath); err == nil {
			return fmt.Errorf("fsops: %s already exists", u.finalPath)
		}
	}
	return os.Rename(u.tmpPath, u.finalPath)
}

func (u *Upload) Abort() error { return u.f.Close() }
