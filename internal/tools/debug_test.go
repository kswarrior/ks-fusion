package tools

import "testing"

func TestDebugBreakpoints(t *testing.T) {
	src := "let x = 1\nx = x + 1\nprint x\n"
	res, err := DebugSource(src, []int{2}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) == 0 {
		t.Fatalf("want breakpoint hit, got %+v", res)
	}
	if res.Hits[0].Line != 2 {
		t.Fatalf("want hit at line 2, got %+v", res.Hits)
	}
	if res.Vars["x"] != "2" {
		t.Fatalf("want x=2, got %v", res.Vars)
	}
}

func TestDebugTrace(t *testing.T) {
	src := "let a = 1\nlet b = 2\n"
	res, err := DebugSource(src, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Trace) < 2 {
		t.Fatalf("want trace >=2, got %v", res.Trace)
	}
}

func TestDebugBadFile(t *testing.T) {
	if _, err := DebugFile("/nonexistent-ks-dbg.ks", nil, false); err == nil {
		t.Fatal("want error for missing file")
	}
}
