package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kswarrior/ks-fusion/internal/backend"
	"github.com/kswarrior/ks-fusion/internal/frontend"
)

// Profiler (v2.6): exact .ks-line execution counts over the tree-walk
// interpreter. Unlike host `--cpuprofile` (Go pprof of the host binary),
// this attributes counts to .ks lines via the OnStmt hook: every executed
// statement increments its line. Deterministic, zero sampling skew — but it
// counts statements, not wall time (a line with 1 heavy builtin call and a
// line with 1 cheap assign both count 1 per execution).

// ProfileLine is one .ks line's execution count.
type ProfileLine struct {
	Line  int
	Count int
	Kind  string // statement kind of last hit (e.g. "assign", "for")
}

// ProfileResult is the outcome of a profile run.
type ProfileResult struct {
	Lines []ProfileLine // sorted: count desc, then line asc
	Total int           // total statement executions
}

// ProfileFile profiles a .ks file: runs it and counts per-line executions.
func ProfileFile(path string) (*ProfileResult, error) {
	prog, err := frontend.ParseFile(path)
	if err != nil {
		return nil, err
	}
	counts := map[int]int{}
	kinds := map[int]string{}
	in := backend.New()
	if d := filepath.Dir(path); d != "" && d != "." {
		if abs, err := filepath.Abs(d); err == nil {
			in.SetBaseDir(abs)
		}
	}
	in.OnStmt = func(line int, kind string) error {
		counts[line]++
		kinds[line] = kind
		return nil
	}
	if err := in.ExecProgram(prog); err != nil {
		return resultFromCounts(counts, kinds), err
	}
	in.WgWait()
	return resultFromCounts(counts, kinds), nil
}

// ProfileSource is ProfileFile over inline source (tests).
func ProfileSource(src string) (*ProfileResult, error) {
	f, err := os.CreateTemp("", "ksprof-*.ks")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(src); err != nil {
		return nil, err
	}
	f.Close()
	return ProfileFile(f.Name())
}

func resultFromCounts(counts map[int]int, kinds map[int]string) *ProfileResult {
	res := &ProfileResult{}
	for line, n := range counts {
		res.Lines = append(res.Lines, ProfileLine{Line: line, Count: n, Kind: kinds[line]})
		res.Total += n
	}
	sort.Slice(res.Lines, func(i, j int) bool {
		if res.Lines[i].Count != res.Lines[j].Count {
			return res.Lines[i].Count > res.Lines[j].Count
		}
		return res.Lines[i].Line < res.Lines[j].Line
	})
	return res
}

// FormatProfile renders the top N lines (0 = all) as `count  line N (kind)`.
func FormatProfile(res *ProfileResult, top int) string {
	var b strings.Builder
	n := len(res.Lines)
	if top > 0 && top < n {
		n = top
	}
	for _, l := range res.Lines[:n] {
		fmt.Fprintf(&b, "%6d  line %d (%s)\n", l.Count, l.Line, l.Kind)
	}
	fmt.Fprintf(&b, "total %d statement executions over %d lines\n", res.Total, len(res.Lines))
	return b.String()
}
