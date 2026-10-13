// Package protocol implements the newline-delimited rompd JSON-RPC transport.
package protocol

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"time"

	"github.com/BRO3886/romp/internal/state"
)

const Version = 1
const MaxFrame = 8 * 1024 * 1024

func (e *Error) Error() string { return e.Message }

// SocketPath is the sole client transport endpoint.
func SocketPath() string { return filepath.Join(state.Dir(), "romp.sock") }

// Scanner enforces the protocol frame limit.
func Scanner(conn net.Conn) *bufio.Scanner {
	s := bufio.NewScanner(conn)
	s.Buffer(make([]byte, 4096), MaxFrame)
	return s
}

// Client owns one initialized daemon connection.
type Client struct {
	conn           net.Conn
	scan           *bufio.Scanner
	mu             sync.Mutex
	next           uint64
	OnNotification func(string, json.RawMessage)
	Info           Initialized
}

// Dial connects and initializes without opening local storage.
func Dial(ctx context.Context, path, name, version string) (*Client, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("connect to rompd: %w; run `romp daemon start`", err)
	}
	c := &Client{conn: conn, scan: Scanner(conn)}
	err = c.Call(ctx, "initialize", Initialize{name, version, Version, Version, []string{"notifications"}}, &c.Info)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if c.Info.ProtocolVersion != Version {
		conn.Close()
		return nil, fmt.Errorf("incompatible daemon protocol %d; update this client", c.Info.ProtocolVersion)
	}
	return c, nil
}

// Close disconnects without affecting server jobs.
func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) deadline(ctx context.Context) func() {
	if d, ok := ctx.Deadline(); ok {
		c.conn.SetDeadline(d)
	} else {
		c.conn.SetDeadline(time.Time{})
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { c.conn.SetDeadline(time.Now()); close(done) })
	return func() {
		if !stop() {
			<-done
		}
		c.conn.SetDeadline(time.Time{})
	}
}

// Call sends one request and delivers intervening notifications.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	reset := c.deadline(ctx)
	defer reset()
	c.next++
	id := json.RawMessage(fmt.Sprintf("%d", c.next))
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(c.conn).Encode(Message{JSONRPC: "2.0", ID: id, Method: method, Params: p}); err != nil {
		return err
	}
	for c.scan.Scan() {
		var m Message
		if err := json.Unmarshal(c.scan.Bytes(), &m); err != nil {
			return fmt.Errorf("decode daemon response: %w", err)
		}
		if m.JSONRPC != "2.0" {
			return fmt.Errorf("invalid daemon JSON-RPC version")
		}
		if m.Method != "" && len(m.ID) == 0 {
			c.notify(m)
			continue
		}
		if string(m.ID) != string(id) {
			return fmt.Errorf("unexpected daemon response id %s", m.ID)
		}
		if m.Error != nil {
			return m.Error
		}
		if result == nil {
			return nil
		}
		return json.Unmarshal(m.Result, result)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := c.scan.Err(); err != nil {
		return err
	}
	return fmt.Errorf("rompd disconnected")
}

func (c *Client) notify(m Message) {
	if c.OnNotification != nil {
		c.OnNotification(m.Method, m.Params)
	}
}

// Listen delivers subscription notifications until cancellation or disconnect.
func (c *Client) Listen(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	reset := c.deadline(ctx)
	defer reset()
	for c.scan.Scan() {
		var m Message
		if err := json.Unmarshal(c.scan.Bytes(), &m); err != nil {
			return err
		}
		if m.JSONRPC != "2.0" || m.Method == "" || len(m.ID) != 0 {
			return fmt.Errorf("invalid daemon notification")
		}
		c.notify(m)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := c.scan.Err(); err != nil {
		return err
	}
	return fmt.Errorf("rompd disconnected")
}
