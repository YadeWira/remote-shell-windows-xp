package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/BaseMax/remote-shell-windows-xp/internal/protocol"
)

func cmdLs(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: xpctl ls <addr:port> <path>")
	}
	var res protocol.FSListResult
	if err := simpleRequest(args[0], protocol.MsgFSList, protocol.Marshal(protocol.FSListRequest{Path: args[1]}), &res); err != nil {
		return err
	}
	printListing(res)
	return nil
}

func printListing(res protocol.FSListResult) {
	for _, e := range res.Entries {
		kind := "F"
		if e.IsDir {
			kind = "D"
		}
		fmt.Printf("%s  %12d  %s  %s\n", kind, e.Size, e.ModTime, e.Name)
	}
}

func cmdStat(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: xpctl stat <addr:port> <path>")
	}
	var res protocol.FSStatResult
	if err := simpleRequest(args[0], protocol.MsgFSStat, protocol.Marshal(protocol.FSStatRequest{Path: args[1]}), &res); err != nil {
		return err
	}
	printStat(res)
	return nil
}

func printStat(res protocol.FSStatResult) {
	if !res.Exists {
		fmt.Println("does not exist")
		return
	}
	kind := "file"
	if res.IsDir {
		kind = "directory"
	}
	fmt.Printf("%s, %d bytes, modified %s\n", kind, res.Size, res.ModTime)
}

func cmdMkdir(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: xpctl mkdir <addr:port> <path>")
	}
	return simpleRequest(args[0], protocol.MsgFSMkdir, protocol.Marshal(protocol.FSMkdirRequest{Path: args[1]}), nil)
}

func cmdRm(args []string) error {
	fs := flag.NewFlagSet("rm", flag.ExitOnError)
	recursive := fs.Bool("recursive", false, "remove directories and their contents")
	fs.Parse(reorderArgs(fs, args))
	rest := fs.Args()
	if len(rest) < 2 {
		return fmt.Errorf("usage: xpctl rm <addr:port> <path> [--recursive]")
	}
	return simpleRequest(rest[0], protocol.MsgFSDelete, protocol.Marshal(protocol.FSDeleteRequest{Path: rest[1], Recursive: *recursive}), nil)
}

func cmdMv(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: xpctl mv <addr:port> <src> <dst>")
	}
	return simpleRequest(args[0], protocol.MsgFSMove, protocol.Marshal(protocol.FSMoveRequest{Src: args[1], Dst: args[2]}), nil)
}

func cmdCp(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: xpctl cp <addr:port> <src> <dst>")
	}
	return simpleRequest(args[0], protocol.MsgFSCopy, protocol.Marshal(protocol.FSCopyRequest{Src: args[1], Dst: args[2]}), nil)
}

func cmdGet(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: xpctl get <addr:port> <remote-path> <local-path>")
	}
	addr, remotePath, localPath := args[0], args[1], args[2]

	out, err := os.OpenFile(localPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer out.Close()

	var total int64
	err = withClient(addr, func(c *Client) error {
		var derr error
		total, derr = streamRemoteFile(c, remotePath, out)
		return derr
	})
	if err != nil {
		return err
	}

	fmt.Printf("downloaded %d bytes to %s\n", total, localPath)
	return nil
}

func cmdCat(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: xpctl cat <addr:port> <remote-path> [<remote-path>...]")
	}
	addr, paths := args[0], args[1:]

	return withClient(addr, func(c *Client) error {
		for _, p := range paths {
			if _, err := streamRemoteFile(c, p, os.Stdout); err != nil {
				return err
			}
		}
		return nil
	})
}

func streamRemoteFile(c *Client, remotePath string, out io.Writer) (int64, error) {
	id := c.mux.NextStreamID()
	ch := c.mux.Open(id)
	defer c.mux.CloseStream(id)

	req := protocol.FSReadStartRequest{Path: remotePath}
	if err := c.mux.Send(protocol.Frame{Type: protocol.MsgFSReadStart, StreamID: id, Payload: protocol.Marshal(req)}); err != nil {
		return 0, err
	}
	return receiveDownload(c, id, ch, out)
}

func receiveDownload(c *Client, id uint32, ch <-chan protocol.Frame, out io.Writer) (int64, error) {
	hasher := sha256.New()
	var total int64
	for f := range ch {
		switch f.Type {
		case protocol.MsgFSReadChunk:
			hasher.Write(f.Payload)
			if _, err := out.Write(f.Payload); err != nil {
				return 0, err
			}
			total += int64(len(f.Payload))
		case protocol.MsgFSReadDone:
			var res protocol.FSReadDoneResult
			protocol.Unmarshal(f.Payload, &res)
			c.mux.Send(protocol.Frame{Type: protocol.MsgStreamEnd, StreamID: id})
			sum := hex.EncodeToString(hasher.Sum(nil))
			if sum != res.Sha256 {
				return 0, fmt.Errorf("checksum mismatch after download: expected %s got %s", res.Sha256, sum)
			}
			return total, nil
		case protocol.MsgError:
			return 0, errorFromFrame(f)
		}
	}
	return 0, errors.New("connection closed before transfer completed")
}

func cmdPut(args []string) error {
	fs := flag.NewFlagSet("put", flag.ExitOnError)
	overwrite := fs.Bool("overwrite", false, "overwrite the remote file if it already exists")
	resume := fs.Bool("resume", true, "resume a previously interrupted upload if a partial file is found")
	fs.Parse(reorderArgs(fs, args))
	rest := fs.Args()
	if len(rest) < 3 {
		return fmt.Errorf("usage: xpctl put <addr:port> <local-path> <remote-path> [--overwrite] [--resume]")
	}
	addr, localPath, remotePath := rest[0], rest[1], rest[2]

	in, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}

	err = withClient(addr, func(c *Client) error {
		id := c.mux.NextStreamID()
		ch := c.mux.Open(id)
		defer c.mux.CloseStream(id)

		startReq := protocol.FSWriteStartRequest{Path: remotePath, Size: info.Size(), Overwrite: *overwrite, Resume: *resume}
		if err := c.mux.Send(protocol.Frame{Type: protocol.MsgFSWriteStart, StreamID: id, Payload: protocol.Marshal(startReq)}); err != nil {
			return err
		}

		ack, err := awaitWriteAck(ch)
		if err != nil {
			return err
		}

		sum, err := sendFileChunks(c, id, in, ack.Offset)
		if err != nil {
			return err
		}

		return awaitWriteResult(c, id, ch, sum)
	})
	if err != nil {
		return err
	}

	fmt.Printf("uploaded %d bytes to %s\n", info.Size(), remotePath)
	return nil
}

func awaitWriteAck(ch <-chan protocol.Frame) (protocol.FSWriteAckResult, error) {
	select {
	case f, ok := <-ch:
		if !ok {
			return protocol.FSWriteAckResult{}, errors.New("connection closed while waiting for upload ack")
		}
		if f.Type == protocol.MsgError {
			return protocol.FSWriteAckResult{}, errorFromFrame(f)
		}
		var ack protocol.FSWriteAckResult
		protocol.Unmarshal(f.Payload, &ack)
		return ack, nil
	case <-time.After(30 * time.Second):
		return protocol.FSWriteAckResult{}, errors.New("timed out waiting for upload ack")
	}
}

func sendFileChunks(c *Client, id uint32, in *os.File, startOffset int64) (string, error) {
	hasher := sha256.New()
	if startOffset > 0 {
		if _, err := io.CopyN(hasher, in, startOffset); err != nil {
			return "", fmt.Errorf("hashing already-uploaded prefix: %w", err)
		}
	} else if _, err := in.Seek(0, io.SeekStart); err != nil {
		return "", err
	}

	buf := make([]byte, protocol.ChunkSize)
	offset := startOffset
	for {
		n, rerr := in.Read(buf)
		if n > 0 {
			hasher.Write(buf[:n])
			chunk := protocol.EncodeChunk(offset, buf[:n])
			if err := c.mux.Send(protocol.Frame{Type: protocol.MsgFSWriteChunk, StreamID: id, Payload: chunk}); err != nil {
				return "", err
			}
			offset += int64(n)
		}
		if rerr != nil {
			if rerr != io.EOF {
				return "", rerr
			}
			break
		}
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func awaitWriteResult(c *Client, id uint32, ch <-chan protocol.Frame, sha256Hex string) error {
	done := protocol.FSWriteDoneRequest{Sha256: sha256Hex}
	if err := c.mux.Send(protocol.Frame{Type: protocol.MsgFSWriteDone, StreamID: id, Payload: protocol.Marshal(done)}); err != nil {
		return err
	}

	select {
	case f, ok := <-ch:
		if !ok {
			return fmt.Errorf("connection closed: %v", c.mux.Err())
		}
		if f.Type == protocol.MsgError {
			return errorFromFrame(f)
		}
		var res protocol.Result
		protocol.Unmarshal(f.Payload, &res)
		if !res.OK {
			return errors.New(res.Message)
		}
		return nil
	case <-time.After(60 * time.Second):
		return errors.New("timed out waiting for upload result")
	}
}
