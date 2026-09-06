package backend

import (
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"
)

// v2.3 additions: persistent state (use_state), TCP/TLS, WS-minimal.

var globalStateMu sync.RWMutex
var globalState = map[string]Value{}

func extraBuiltinsV23() []*BuiltinObj {
	return []*BuiltinObj{
		{Name: "use_state", Fn: bUseState},
		{Name: "set_state", Fn: bSetState},
		{Name: "on_mount", Fn: bOnMount},
		{Name: "tcp_connect", Fn: bTCPConnect},
		{Name: "tcp_send", Fn: bTCPSend},
		{Name: "tcp_recv", Fn: bTCPRecv},
		{Name: "tcp_close", Fn: bTCPClose},
		{Name: "tcp_serve", Fn: bTCPServe},
		{Name: "tls_connect", Fn: bTLSConnect},
		{Name: "ws_connect", Fn: bWSConnect},
	}
}

func bUseState(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("use_state", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("use_state key must be string, got %s", TypeName(args[0]))
	}
	globalStateMu.RLock()
	v, ok := globalState[args[0].Str]
	globalStateMu.RUnlock()
	if ok {
		return v, nil
	}
	globalStateMu.Lock()
	globalState[args[0].Str] = args[1]
	globalStateMu.Unlock()
	return args[1], nil
}

func bSetState(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("set_state", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("set_state key must be string, got %s", TypeName(args[0]))
	}
	globalStateMu.Lock()
	globalState[args[0].Str] = args[1]
	globalStateMu.Unlock()
	return args[1], nil
}

func bOnMount(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("on_mount", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VFunc && args[0].Kind != VBuiltin {
		return Nil(), fmt.Errorf("on_mount wants func, got %s", TypeName(args[0]))
	}
	return in.callValue(args[0], []Value{})
}

// --- TCP (ints as handles via registry) ---

var tcpMu sync.Mutex
var tcpConns = map[int]net.Conn{}
var tcpNext = 1

func tcpRegister(c net.Conn) int {
	tcpMu.Lock()
	defer tcpMu.Unlock()
	id := tcpNext
	tcpNext++
	tcpConns[id] = c
	return id
}

func tcpLookup(id int) (net.Conn, bool) {
	tcpMu.Lock()
	defer tcpMu.Unlock()
	c, ok := tcpConns[id]
	return c, ok
}

func bTCPConnect(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("tcp_connect", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString || args[1].Kind != VInt {
		return Nil(), fmt.Errorf("tcp_connect wants (host, port)")
	}
	addr := fmt.Sprintf("%s:%d", args[0].Str, args[1].Int)
	c, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return Nil(), err
	}
	return IntV(tcpRegister(c)), nil
}

func bTCPSend(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("tcp_send", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VInt || args[1].Kind != VString {
		return Nil(), fmt.Errorf("tcp_send wants (id, data)")
	}
	c, ok := tcpLookup(args[0].Int)
	if !ok {
		return Nil(), fmt.Errorf("tcp_send: bad id %d", args[0].Int)
	}
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	n, err := c.Write([]byte(args[1].Str))
	if err != nil {
		return Nil(), err
	}
	return IntV(n), nil
}

func bTCPRecv(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("tcp_recv", args, 1, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VInt {
		return Nil(), fmt.Errorf("tcp_recv wants id")
	}
	n := 4096
	if len(args) == 2 {
		if args[1].Kind != VInt {
			return Nil(), fmt.Errorf("tcp_recv n must be int")
		}
		n = args[1].Int
		if n <= 0 || n > 1<<20 {
			return Nil(), fmt.Errorf("tcp_recv n out of range")
		}
	}
	c, ok := tcpLookup(args[0].Int)
	if !ok {
		return Nil(), fmt.Errorf("tcp_recv: bad id %d", args[0].Int)
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, n)
	m, err := c.Read(buf)
	if err != nil {
		return Nil(), err
	}
	return StrV(string(buf[:m])), nil
}

func bTCPClose(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("tcp_close", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VInt {
		return Nil(), fmt.Errorf("tcp_close wants id")
	}
	tcpMu.Lock()
	c, ok := tcpConns[args[0].Int]
	if ok {
		delete(tcpConns, args[0].Int)
	}
	tcpMu.Unlock()
	if !ok {
		return Nil(), fmt.Errorf("tcp_close: bad id %d", args[0].Int)
	}
	return Nil(), c.Close()
}

func bTCPServe(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("tcp_serve", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VInt {
		return Nil(), fmt.Errorf("tcp_serve port must be int")
	}
	if args[1].Kind != VFunc && args[1].Kind != VBuiltin {
		return Nil(), fmt.Errorf("tcp_serve handler must be func")
	}
	port := args[0].Int
	handler := args[1]
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return Nil(), err
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			id := tcpRegister(c)
			go func(id int) {
				_, _ = in.callValue(handler, []Value{IntV(id)})
			}(id)
		}
	}()
	return IntV(port), nil
}

func bTLSConnect(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("tls_connect", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString || args[1].Kind != VInt {
		return Nil(), fmt.Errorf("tls_connect wants (host, port)")
	}
	addr := fmt.Sprintf("%s:%d", args[0].Str, args[1].Int)
	c, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", addr, &tls.Config{InsecureSkipVerify: false, ServerName: args[0].Str})
	if err != nil {
		return Nil(), err
	}
	return IntV(tcpRegister(c)), nil
}

// ws_connect minimal: TCP + HTTP Upgrade header, returns conn id (text frames assumed).
func bWSConnect(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("ws_connect", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString || args[1].Kind != VInt {
		return Nil(), fmt.Errorf("ws_connect wants (host, port)")
	}
	// minimal: plain TCP to host:port with Upgrade request sent
	addr := fmt.Sprintf("%s:%d", args[0].Str, args[1].Int)
	c, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return Nil(), err
	}
	req := fmt.Sprintf("GET / HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n", args[0].Str)
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte(req)); err != nil {
		_ = c.Close()
		return Nil(), err
	}
	return IntV(tcpRegister(c)), nil
}
