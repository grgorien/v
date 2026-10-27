package nvrpc

import (
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"v/internal/msgpack"
)

func fakeServer(t *testing.T, sock string, handler func(c net.Conn)) {
	t.Helper()
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handler(c)
		}
	}()
}

func writeMsg(t *testing.T, c net.Conn, v any) {
	t.Helper()
	b, err := msgpack.Marshal(v)
	if err != nil {
		t.Errorf("marshal: %v", err)
		return
	}
	if _, err := c.Write(b); err != nil {
		t.Errorf("write: %v", err)
	}
}

func readReq(t *testing.T, c net.Conn) (int64, string) {
	t.Helper()
	d := msgpack.NewDecoder(c)
	v, err := d.Decode()
	if err != nil {
		t.Errorf("decode: %v", err)
		return 0, ""
	}
	arr := v.([]any)
	id := arr[1].(int64)
	method := arr[2].(string)
	return id, method
}

func TestCallSuccess(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s.sock")
	fakeServer(t, sock, func(c net.Conn) {
		defer c.Close()
		id, _ := readReq(t, c)
		writeMsg(t, c, []any{1, id, nil, int64(7)})
	})
	cl, err := Dial(sock, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cl.Close()
	res, err := cl.Call("foo", nil, time.Second)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res != int64(7) {
		t.Fatalf("got %#v", res)
	}
}

func TestCallError(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s.sock")
	fakeServer(t, sock, func(c net.Conn) {
		defer c.Close()
		id, _ := readReq(t, c)
		writeMsg(t, c, []any{1, id, []any{int64(1), "boom"}, nil})
	})
	cl, err := Dial(sock, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cl.Close()
	_, err = cl.Call("foo", nil, time.Second)
	var rpcErr *Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *Error, got %v", err)
	}
	if rpcErr.Message != "boom" || rpcErr.Kind != 1 {
		t.Fatalf("unexpected error: %#v", rpcErr)
	}
}

func TestCallTimeout(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s.sock")
	fakeServer(t, sock, func(c net.Conn) {
		defer c.Close()
		_, _ = readReq(t, c)
		time.Sleep(2 * time.Second)
	})
	cl, err := Dial(sock, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cl.Close()
	_, err = cl.Call("foo", nil, 100*time.Millisecond)
	if !IsTimeout(err) {
		t.Fatalf("expected timeout, got %v", err)
	}
}

func TestSkipsNotificationAndRequest(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s.sock")
	fakeServer(t, sock, func(c net.Conn) {
		defer c.Close()
		id, _ := readReq(t, c)
		writeMsg(t, c, []any{2, "note", []any{}})
		writeMsg(t, c, []any{0, int64(99), "srv", []any{}})
		writeMsg(t, c, []any{1, id, nil, "ok"})
	})
	cl, err := Dial(sock, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cl.Close()
	res, err := cl.Call("foo", nil, time.Second)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res != "ok" {
		t.Fatalf("got %#v", res)
	}
}

func TestIsRefusedMissing(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "nope.sock")
	_, err := Dial(sock, 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsRefused(err) {
		t.Fatalf("expected IsRefused, got %v", err)
	}
}

func TestIsClosedAfterClose(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s.sock")
	fakeServer(t, sock, func(c net.Conn) {
		defer c.Close()
		_, _ = readReq(t, c)
	})
	cl, err := Dial(sock, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cl.Close()
	_, err = cl.Call("foo", nil, 100*time.Millisecond)
	if !IsClosed(err) {
		t.Fatalf("expected closed, got %v", err)
	}
}
