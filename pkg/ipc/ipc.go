package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
)

// Request represents an IPC request.
type Request struct {
	Method  string          `json:"method"`
	Name    string          `json:"name,omitempty"`
	Format  string          `json:"format,omitempty"`
	Context json.RawMessage `json:"context,omitempty"`
}

// Response represents an IPC response.
type Response struct {
	OK    bool        `json:"ok"`
	Error string      `json:"error,omitempty"`
	Data  interface{} `json:"data,omitempty"`
}

// Conn wraps a Unix socket connection with framed JSON.
type Conn struct {
	conn net.Conn
	rw   *bufio.ReadWriter
}

// Dial connects to a Unix socket.
func Dial(socketPath string) (*Conn, error) {
	c, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, err
	}
	return &Conn{conn: c, rw: bufio.NewReadWriter(bufio.NewReader(c), bufio.NewWriter(c))}, nil
}

// Close closes the connection.
func (c *Conn) Close() error {
	return c.conn.Close()
}

// SendRequest writes a framed JSON request.
func (c *Conn) SendRequest(req Request) error {
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if _, err := c.rw.Write(append(b, '\n')); err != nil {
		return err
	}
	return c.rw.Flush()
}

// ReadResponse reads one framed JSON response.
func (c *Conn) ReadResponse(resp interface{}) error {
	line, err := c.rw.ReadBytes('\n')
	if err != nil {
		return err
	}
	if err := json.Unmarshal(line, resp); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}
	return nil
}
