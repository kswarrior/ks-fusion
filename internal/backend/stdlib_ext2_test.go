package backend

import (
	"testing"

	"github.com/kswarrior/ks-fusion/internal/frontend"
)

func mustRunV23(t *testing.T, src string) {
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

func TestV23State(t *testing.T) {
	mustRunV23(t, `set_state("tkey", 41)`+"\n"+`assert(use_state("tkey", 0) == 41)`)
	mustRunV23(t, `assert(use_state("fresh_xyz_123", "dflt") == "dflt")`)
	mustRunV23(t, `on_mount(func() { return 1 })`)
}

func TestV23TCP(t *testing.T) {
	// tcp_serve + connect/send/recv roundtrip on localhost.
	// Port 0 asks the OS for a free port; tcp_serve returns the bound
	// port and tcp_shutdown releases it, so `go test -count=N` is repeat-safe.
	mustRunV23(t, `
let port = tcp_serve(0, func(id) {
  let msg = tcp_recv(id, 64)
  tcp_send(id, "echo:" + msg)
  tcp_close(id)
})
assert(port > 0)
sleep(100)
let c = tcp_connect("127.0.0.1", port)
tcp_send(c, "hi")
let r = tcp_recv(c, 64)
assert(r == "echo:hi")
tcp_close(c)
tcp_shutdown(port)
`)
}

func TestTCPShutdownUnknown(t *testing.T) {
	p, err := frontend.ParseSource(`tcp_shutdown(1)`, "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(p); err == nil {
		t.Fatal("want error for unknown listener port")
	}
}

func TestV23BuiltinCount(t *testing.T) {
	if BuiltinCount() < 158 {
		t.Fatalf("want >=158 builtins after v2.3, got %d", BuiltinCount())
	}
}
