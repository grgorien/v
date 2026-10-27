// implements a minimal MessagePack-RPC client for Neovim.
package nvrpc

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"syscall"
	"time"

	"v/internal/msgpack"
)

// Error is an RPC error returned by the server.
type Error struct {
	Kind    int64
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Kind, e.Message)
}

// Client is a single-connection RPC client.
type Client struct {
	conn  net.Conn
	dec   *msgpack.Decoder
	mu    sync.Mutex
	msgid int64
}

// Dial connects to the unix socket at sock with the given timeout.
func Dial(sock string, timeout time.Duration) (*Client, error) {
	conn, err := net.DialTimeout("unix", sock, timeout)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, dec: msgpack.NewDecoder(conn)}, nil
}

// Close closes the underlying connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// Call sends a request and waits for the matching response.
func (c *Client) Call(method string, params []any, timeout time.Duration) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if params == nil {
		params = []any{}
	}
	c.msgid++
	id := c.msgid
	req := []any{0, id, method, params}
	b, err := msgpack.Marshal(req)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	if err := c.conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	if _, err := c.conn.Write(b); err != nil {
		return nil, err
	}
	for {
		v, err := c.dec.Decode()
		if err != nil {
			return nil, err
		}
		arr, ok := v.([]any)
		if !ok || len(arr) == 0 {
			continue
		}
		typ, ok := arr[0].(int64)
		if !ok {
			continue
		}
		switch typ {
		case 0, 2:
			// Incoming request or notification: skip.
			continue
		case 1:
			if len(arr) < 4 {
				return nil, errors.New("nvrpc: malformed response")
			}
			rid, ok := arr[1].(int64)
			if !ok || rid != id {
				continue
			}
			if arr[2] != nil {
				if ea, ok := arr[2].([]any); ok && len(ea) >= 2 {
					kind, _ := ea[0].(int64)
					msg, _ := ea[1].(string)
					return nil, &Error{Kind: kind, Message: msg}
				}
				return nil, &Error{Kind: 0, Message: fmt.Sprint(arr[2])}
			}
			return arr[3], nil
		}
	}
}

// ExecLua calls nvim_exec_lua.
func (c *Client) ExecLua(code string, args []any, timeout time.Duration) (any, error) {
	if args == nil {
		args = []any{}
	}
	return c.Call("nvim_exec_lua", []any{code, args}, timeout)
}

// Command calls nvim_command.
func (c *Client) Command(cmd string, timeout time.Duration) error {
	_, err := c.Call("nvim_command", []any{cmd}, timeout)
	return err
}

// IsRefused reports whether err is a connection-refused or not-found error.
func IsRefused(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT) {
		return true
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	var ne *net.OpError
	if errors.As(err, &ne) {
		if errors.Is(ne.Err, syscall.ECONNREFUSED) || errors.Is(ne.Err, syscall.ENOENT) {
			return true
		}
	}
	return false
}

// IsTimeout reports whether err is a timeout.
func IsTimeout(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return errors.Is(err, os.ErrDeadlineExceeded)
}

// IsClosed reports whether err indicates the peer closed the connection.
func IsClosed(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	return false
}
