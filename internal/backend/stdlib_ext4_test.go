package backend

import (
	"fmt"
	"net"
	"testing"

	"github.com/kswarrior/ks-fusion/internal/frontend"
)

func mustRunV25(t *testing.T, src string) {
	t.Helper()
	p, err := frontend.ParseSource(src, "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	frontend.FoldProgram(p)
	if err := Run(p); err != nil {
		t.Fatal(err)
	}
}

// serverTextFrame builds an unmasked server-to-client text frame (single shot).
func serverTextFrame(s string) []byte {
	p := []byte(s)
	out := []byte{0x81}
	if len(p) < 126 {
		out = append(out, byte(len(p)))
	} else {
		out = append(out, 126, byte(len(p)>>8), byte(len(p)))
	}
	return append(out, p...)
}

func TestWSFramesRoundtrip(t *testing.T) {
	// .ks -> Go: ws_send must emit a masked client text frame decodable per RFC 6455.
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	id := tcpRegister(c1)
	defer tcpUnregister(id)
	done := make(chan error, 1)
	go func() {
		_, op, payload, err := wsReadFrame(c2)
		if err != nil {
			done <- err
			return
		}
		if op != 0x1 || string(payload) != "hello ws" {
			done <- fmt.Errorf("got op=%d payload=%q", op, payload)
			return
		}
		done <- nil
	}()
	mustRunV25(t, fmt.Sprintf("ws_send(%d, \"hello ws\")", id))
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestWSRecvSkipsPing(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	id := tcpRegister(c1)
	defer tcpUnregister(id)
	go func() {
		// ping with empty payload, then a fragmented text message "hi!"
		_, _ = c2.Write(wsEncodeControl(0x9, nil))
		_, _ = c2.Write([]byte{0x01, 2, 'h', 'i'})
		_, _ = c2.Write([]byte{0x80, 1, '!'})
	}()
	mustRunV25(t, fmt.Sprintf("let r = ws_recv(%d)\nassert(r == \"hi!\")", id))
}

func TestWSRecvCloseIsError(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	id := tcpRegister(c1)
	defer tcpUnregister(id)
	go func() {
		_, _ = c2.Write(wsEncodeControl(0x8, nil))
	}()
	p, err := frontend.ParseSource(fmt.Sprintf("ws_recv(%d)", id), "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(p); err == nil {
		t.Fatal("want error on close frame")
	}
}

func TestExecPipesSplit(t *testing.T) {
	mustRunV25(t, `
let r = exec_pipes("sh", ["-c", "echo out; echo err 1>&2"], nil)
assert(r.code == 0)
assert(r.stdout == "out\\n")
assert(r.stderr == "err\\n")
let e = exec_pipes("sh", ["-c", "cat"], "hello stdin")
assert(e.stdout == "hello stdin")
assert(e.code == 0)
`)
}

func TestSpawnWaitKill(t *testing.T) {
	mustRunV25(t, `
let id = spawn("sh", ["-c", "echo hi"])
let r = proc_wait(id)
assert(r.code == 0)
assert(r.stdout == "hi\\n")
`)
	mustRunV25(t, `
let id = spawn("sh", ["-c", "sleep 30"])
proc_kill(id, "KILL")
let r = proc_wait(id)
assert(r.code != 0)
`)
}

func TestV25BuiltinCount(t *testing.T) {
	if BuiltinCount() < 172 {
		t.Fatalf("want >=172 builtins after v2.5, got %d", BuiltinCount())
	}
}
