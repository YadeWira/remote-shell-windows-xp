package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/BaseMax/remote-shell-windows-xp/internal/protocol"
	"github.com/BaseMax/remote-shell-windows-xp/internal/transport"
	"github.com/BaseMax/remote-shell-windows-xp/internal/trust"
)

type Client struct {
	conn *transport.Conn
	mux  *transport.Multiplexer
}

func connect(addr string) (*Client, error) {
	path, err := trust.DefaultPath()
	if err != nil {
		return nil, err
	}
	store, err := trust.Load(path)
	if err != nil {
		return nil, err
	}
	entry, ok := store.Get(addr)
	if !ok {
		return nil, fmt.Errorf("no saved trust for %s, run `xpctl pair %s --token <token>` first", addr, addr)
	}

	conn, _, err := transport.DialTLS(addr, entry.Fingerprint, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", addr, err)
	}
	if _, err := transport.ClientHandshakeAs(conn, entry.Token, clientName()); err != nil {
		conn.Close()
		return nil, fmt.Errorf("authenticating with %s: %w", addr, err)
	}

	mux := transport.NewMultiplexer(conn)
	go mux.Run()
	return &Client{conn: conn, mux: mux}, nil
}

func (c *Client) Close() error { return c.mux.Close() }

// clientName identifies this controller to the agent's audit log: $XPCTL_NAME, or user@host.
func clientName() string {
	if n := os.Getenv("XPCTL_NAME"); n != "" {
		return n
	}
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("USERNAME")
	}
	host, _ := os.Hostname()
	switch {
	case user != "" && host != "":
		return user + "@" + host
	case user != "":
		return user
	}
	return host
}

func withClient(addr string, fn func(*Client) error) error {
	c, err := connect(addr)
	if err != nil {
		return err
	}
	defer c.Close()
	return fn(c)
}

func simpleRequest(addr string, reqType byte, payload []byte, out interface{}) error {
	return withClient(addr, func(c *Client) error {
		f, err := c.request(reqType, payload)
		if err != nil {
			return err
		}
		if out != nil {
			protocol.Unmarshal(f.Payload, out)
		}
		return nil
	})
}

func (c *Client) request(reqType byte, payload []byte) (protocol.Frame, error) {
	id := c.mux.NextStreamID()
	ch := c.mux.Open(id)
	defer c.mux.CloseStream(id)

	if err := c.mux.Send(protocol.Frame{Type: reqType, StreamID: id, Payload: payload}); err != nil {
		return protocol.Frame{}, err
	}

	select {
	case f, ok := <-ch:
		if !ok {
			return protocol.Frame{}, fmt.Errorf("connection closed: %v", c.mux.Err())
		}
		if f.Type == protocol.MsgError {
			return protocol.Frame{}, errorFromFrame(f)
		}
		return f, nil
	case <-time.After(30 * time.Second):
		return protocol.Frame{}, errors.New("timed out waiting for response")
	}
}

func errorFromFrame(f protocol.Frame) error {
	var e protocol.ErrorPayload
	protocol.Unmarshal(f.Payload, &e)
	return errors.New(e.Message)
}
