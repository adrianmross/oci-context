package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"

	ipcmsg "github.com/adrianmross/oci-context/pkg/ipc"
)

// HandlerFunc processes a request and returns a response payload or error.
type HandlerFunc func(req ipcmsg.Request) (interface{}, error)

// Serve starts a Unix socket server and handles requests with the provided handler.
func Serve(socketPath string, handler HandlerFunc) error {
	// remove stale socket
	if err := os.RemoveAll(socketPath); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer ln.Close()
	if err := os.Chmod(socketPath, 0o600); err != nil {
		return fmt.Errorf("chmod socket: %w", err)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			return fmt.Errorf("accept: %w", err)
		}
		go handleConn(conn, handler)
	}
}

func handleConn(c net.Conn, handler HandlerFunc) {
	defer c.Close()
	rw := bufio.NewReadWriter(bufio.NewReader(c), bufio.NewWriter(c))
	for {
		line, err := rw.ReadBytes('\n')
		if err != nil {
			return
		}
		var req ipcmsg.Request
		if err := json.Unmarshal(line, &req); err != nil {
			writeResp(rw, ipcmsg.Response{OK: false, Error: "invalid request"})
			continue
		}
		data, err := handler(req)
		if err != nil {
			writeResp(rw, ipcmsg.Response{OK: false, Error: err.Error()})
			continue
		}
		writeResp(rw, ipcmsg.Response{OK: true, Data: data})
	}
}

func writeResp(w *bufio.ReadWriter, resp ipcmsg.Response) {
	b, err := json.Marshal(resp)
	if err != nil {
		return
	}
	b = append(b, '\n')
	_, _ = w.Write(b)
	_ = w.Flush()
}

// ErrNotImplemented is returned for unknown methods.
var ErrNotImplemented = errors.New("method not implemented")
