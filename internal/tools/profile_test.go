package tools

import "testing"

func TestProfileCountsLoop(t *testing.T) {
	src := "let s = 0\nfor i in range(5) {\n s = s + 1\n}\nprint s\n"
	res, err := ProfileSource(src)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total < 8 {
		t.Fatalf("want total >=8 statement executions, got %d (%v)", res.Total, res.Lines)
	}
	// the loop body line (3) must execute exactly 5 times
	found := false
	for _, l := range res.Lines {
		if l.Line == 3 {
			found = true
			if l.Count != 5 {
				t.Fatalf("want line 3 x5, got %+v", l)
			}
		}
	}
	if !found {
		t.Fatalf("want line 3 in profile, got %+v", res.Lines)
	}
	// sorted desc: first entry has the max count
	for i := 1; i < len(res.Lines); i++ {
		if res.Lines[i].Count > res.Lines[0].Count {
			t.Fatalf("want sorted desc, got %+v", res.Lines)
		}
	}
}

func TestProfileBadFile(t *testing.T) {
	if _, err := ProfileFile("/nonexistent-ks-prof.ks"); err == nil {
		t.Fatal("want error for missing file")
	}
}

func TestProfileFormat(t *testing.T) {
	res, err := ProfileSource("let x = 1\nprint x\n")
	if err != nil {
		t.Fatal(err)
	}
	s := FormatProfile(res, 1)
	if s == "" {
		t.Fatal("want non-empty profile output")
	}
}
