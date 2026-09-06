package backend

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"
)

// v2.5 additions: RFC 6455 WebSocket framing over `ws_connect` ids,
// split-stream exec (`exec_pipes`), background processes + signals.

// ---------------------------------------------------------------------------
// WebSocket frames (RFC 6455 §5). `ws_connect` returns a tcp-registry id;
// `ws_send` writes one masked client text frame, `ws_recv` returns the next
// complete text message (continuations reassembled, ping/pong handled).
// ---------------------------------------------------------------------------

const wsMaxPayload = 8 << 20 // 8 MiB guard per message

func wsEncodeText(text string) ([]byte, error) {
	payload := []byte(text)
	if len(payload) > wsMaxPayload {
		return nil, fmt.Errorf("ws_send: message too large (%d bytes)", len(payload))
	}
	var key [4]byte
	if _, err := rand.Read(key[:]); err != nil {
		return nil, err
	}
	var out []byte
	out = append(out, 0x81) // FIN + text
	switch {
	case len(payload) < 126:
		out = append(out, 0x80|byte(len(payload)))
	case len(payload) < 1<<16:
		out = append(out, 0x80|126, byte(len(payload)>>8), byte(len(payload)))
	default:
		out = append(out, 0x80|127)
		n := uint64(len(payload))
		for i := 7; i >= 0; i-- {
			out = append(out, byte(n>>(8*i)))
		}
	}
	out = append(out, key[:]...)
	masked := make([]byte, len(payload))
	for i := range payload {
		masked[i] = payload[i] ^ key[i%4]
	}
	return append(out, masked...), nil
}

type wsMessage struct {
	opcode  int
	payload []byte
}

// wsReadFrame reads one frame; caller handles control/data split.
func wsReadFrame(r io.Reader) (fin bool, opcode int, payload []byte, err error) {
	var hdr [2]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return false, 0, nil, err
	}
	fin = hdr[0]&0x80 != 0
	opcode = int(hdr[0] & 0x0f)
	masked := hdr[1]&0x80 != 0
	n := int64(hdr[1] & 0x7f)
	switch n {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return false, 0, nil, err
		}
		n = int64(ext[0])<<8 | int64(ext[1])
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return false, 0, nil, err
		}
		n = 0
		for _, b := range ext {
			n = n<<8 | int64(b)
		}
	}
	if n < 0 || n > wsMaxPayload {
		return false, 0, nil, fmt.Errorf("ws_recv: bad payload length %d", n)
	}
	var key [4]byte
	if masked {
		if _, err = io.ReadFull(r, key[:]); err != nil {
			return false, 0, nil, err
		}
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(r, payload); err != nil {
		return false, 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= key[i%4]
		}
	}
	return fin, opcode, payload, nil
}

// wsEncodeControl builds an unmasked server-style control frame (tests/peers).
func wsEncodeControl(opcode int, payload []byte) []byte {
	out := []byte{0x80 | byte(opcode), byte(len(payload))}
	return append(out, payload...)
}

// wsReadText returns the next complete text message, reassembling fragments
// and answering pings with pongs on w (nil w disables pong replies).
func wsReadText(r io.Reader, w io.Writer) (string, error) {
	var msg []byte
	for {
		fin, op, payload, err := wsReadFrame(r)
		if err != nil {
			return "", err
		}
		switch op {
		case 0x8: // close
			if w != nil {
				_, _ = w.Write(wsEncodeControl(0x8, payload))
			}
			return "", fmt.Errorf("ws_recv: peer closed")
		case 0x9: // ping
			if w != nil {
				_, _ = w.Write(wsEncodeControl(0xA, payload))
			}
			continue
		case 0xA: // pong
			continue
		case 0x1: // text start
			msg = append(msg, payload...)
			if fin {
				return string(msg), nil
			}
		case 0x0: // continuation
			msg = append(msg, payload...)
			if fin {
				return string(msg), nil
			}
		case 0x2:
			return "", fmt.Errorf("ws_recv: binary frames not supported (text only)")
		default:
			return "", fmt.Errorf("ws_recv: unknown opcode %d", op)
		}
		if len(msg) > wsMaxPayload {
			return "", fmt.Errorf("ws_recv: message too large")
		}
	}
}

func bWSSend(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("ws_send", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VInt || args[1].Kind != VString {
		return Nil(), fmt.Errorf("ws_send wants (id, text)")
	}
	c, ok := tcpLookup(args[0].Int)
	if !ok {
		return Nil(), fmt.Errorf("ws_send: bad id %d", args[0].Int)
	}
	frame, err := wsEncodeText(args[1].Str)
	if err != nil {
		return Nil(), err
	}
	_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	n, err := c.Write(frame)
	if err != nil {
		return Nil(), err
	}
	return IntV(n), nil
}

func bWSRecv(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("ws_recv", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VInt {
		return Nil(), fmt.Errorf("ws_recv wants id")
	}
	c, ok := tcpLookup(args[0].Int)
	if !ok {
		return Nil(), fmt.Errorf("ws_recv: bad id %d", args[0].Int)
	}
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	s, err := wsReadText(c, c)
	if err != nil {
		return Nil(), err
	}
	return StrV(s), nil
}

// ---------------------------------------------------------------------------
// Processes: split stdout/stderr + stdin input, background spawn + signals.
// ---------------------------------------------------------------------------

func execArgv(args []Value) ([]string, error) {
	var argv []string
	for _, e := range args {
		if e.Kind != VString {
			return nil, fmt.Errorf("args must be strings")
		}
		argv = append(argv, e.Str)
	}
	return argv, nil
}

// bExecPipes runs cmd with separate stdout/stderr buffers and optional stdin:
// `exec_pipes(cmd, argsArray, inputString?)` -> {code, stdout, stderr}.
func bExecPipes(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("exec_pipes", args, 1, 3); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("exec_pipes wants cmd string, got %s", TypeName(args[0]))
	}
	var argv []string
	if len(args) >= 2 && args[1].Kind != VNil {
		if args[1].Kind != VArray {
			return Nil(), fmt.Errorf("exec_pipes args must be array, got %s", TypeName(args[1]))
		}
		args[1].Arr.Mu.RLock()
		items := make([]Value, len(args[1].Arr.Items))
		copy(items, args[1].Arr.Items)
		args[1].Arr.Mu.RUnlock()
		var err error
		argv, err = execArgv(items)
		if err != nil {
			return Nil(), err
		}
	}
	cmd := exec.Command(args[0].Str, argv...)
	if len(args) == 3 && args[2].Kind != VNil {
		if args[2].Kind != VString {
			return Nil(), fmt.Errorf("exec_pipes input must be string, got %s", TypeName(args[2]))
		}
		cmd.Stdin = bytes.NewBufferString(args[2].Str)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			return Nil(), err
		}
	}
	return MapV(map[string]Value{
		"code":   IntV(code),
		"stdout": StrV(stdout.String()),
		"stderr": StrV(stderr.String()),
	}), nil
}

var procMu sync.Mutex
var procs = map[int]*exec.Cmd{}
var procOut = map[int]*bytes.Buffer{}
var procErr = map[int]*bytes.Buffer{}
var procNext = 1

func lookupSignal(name string) (os.Signal, error) {
	switch name {
	case "KILL":
		return syscall.SIGKILL, nil
	case "TERM":
		return syscall.SIGTERM, nil
	case "INT":
		return syscall.SIGINT, nil
	case "HUP":
		return syscall.SIGHUP, nil
	case "QUIT":
		return syscall.SIGQUIT, nil
	}
	return nil, fmt.Errorf("unknown signal %q (want KILL|TERM|INT|HUP|QUIT)", name)
}

// bSpawn starts `spawn(cmd, argsArray)` in the background and returns a
// process id for `proc_wait`/`proc_kill`.
func bSpawn(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("spawn", args, 1, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("spawn wants cmd string, got %s", TypeName(args[0]))
	}
	var argv []string
	if len(args) == 2 {
		if args[1].Kind != VArray {
			return Nil(), fmt.Errorf("spawn args must be array, got %s", TypeName(args[1]))
		}
		args[1].Arr.Mu.RLock()
		items := make([]Value, len(args[1].Arr.Items))
		copy(items, args[1].Arr.Items)
		args[1].Arr.Mu.RUnlock()
		var err error
		argv, err = execArgv(items)
		if err != nil {
			return Nil(), err
		}
	}
	cmd := exec.Command(args[0].Str, argv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return Nil(), err
	}
	procMu.Lock()
	procNext++
	id := procNext
	procs[id] = cmd
	procOut[id] = &stdout
	procErr[id] = &stderr
	procMu.Unlock()
	return IntV(id), nil
}

// bProcWait blocks until `proc_wait(id)` finishes:
// -> {code, stdout, stderr}.
func bProcWait(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("proc_wait", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VInt {
		return Nil(), fmt.Errorf("proc_wait wants id, got %s", TypeName(args[0]))
	}
	procMu.Lock()
	cmd, ok := procs[args[0].Int]
	out, outOk := procOut[args[0].Int]
	errBuf, errOk := procErr[args[0].Int]
	procMu.Unlock()
	if !ok || !outOk || !errOk {
		return Nil(), fmt.Errorf("proc_wait: bad id %d", args[0].Int)
	}
	err := cmd.Wait()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			return Nil(), err
		}
	}
	procMu.Lock()
	delete(procs, args[0].Int)
	delete(procOut, args[0].Int)
	delete(procErr, args[0].Int)
	procMu.Unlock()
	_ = runtime.GOOS
	return MapV(map[string]Value{
		"code":   IntV(code),
		"stdout": StrV(out.String()),
		"stderr": StrV(errBuf.String()),
	}), nil
}

// bProcKill sends `proc_kill(id, "TERM"|...)` (default TERM) to a spawned process.
func bProcKill(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("proc_kill", args, 1, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VInt {
		return Nil(), fmt.Errorf("proc_kill wants id, got %s", TypeName(args[0]))
	}
	sigName := "TERM"
	if len(args) == 2 {
		if args[1].Kind != VString {
			return Nil(), fmt.Errorf("proc_kill signal must be string, got %s", TypeName(args[1]))
		}
		sigName = args[1].Str
	}
	sig, err := lookupSignal(sigName)
	if err != nil {
		return Nil(), err
	}
	procMu.Lock()
	cmd, ok := procs[args[0].Int]
	procMu.Unlock()
	if !ok {
		return Nil(), fmt.Errorf("proc_kill: bad id %d", args[0].Int)
	}
	if cmd.Process == nil {
		return Nil(), fmt.Errorf("proc_kill: process not started")
	}
	return Nil(), cmd.Process.Signal(sig)
}

func extraBuiltinsV25() []*BuiltinObj {
	return []*BuiltinObj{
		{Name: "ws_send", Fn: bWSSend},
		{Name: "ws_recv", Fn: bWSRecv},
		{Name: "exec_pipes", Fn: bExecPipes},
		{Name: "spawn", Fn: bSpawn},
		{Name: "proc_wait", Fn: bProcWait},
		{Name: "proc_kill", Fn: bProcKill},
	}
}
