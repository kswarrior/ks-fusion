package backend

import "testing"

func TestV24BuiltinCount(t *testing.T) {
	if BuiltinCount() < 166 {
		t.Fatalf("want >=166 builtins after v2.4, got %d", BuiltinCount())
	}
}
