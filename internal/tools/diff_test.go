package tools

import (
	"encoding/json"
	"testing"
)

func mustDiff(t *testing.T, oldVM, newVM string) []PatchOp {
	t.Helper()
	ops, err := DiffViewModels(oldVM, newVM)
	if err != nil {
		t.Fatal(err)
	}
	return ops
}

func opKinds(ops []PatchOp) map[string]int {
	m := map[string]int{}
	for _, o := range ops {
		m[o.Op]++
	}
	return m
}

func TestDiffIdentical(t *testing.T) {
	vm := `{"key":"home","type":"page","props":{"title":"hi"},"children":[]}`
	if ops := mustDiff(t, vm, vm); len(ops) != 0 {
		t.Fatalf("want no ops, got %v", ops)
	}
}

func TestDiffSetProp(t *testing.T) {
	oldVM := `{"key":"home","type":"page","props":{"title":"a"},"children":[]}`
	newVM := `{"key":"home","type":"page","props":{"title":"b"},"children":[]}`
	ops := mustDiff(t, oldVM, newVM)
	if len(ops) != 1 || ops[0].Op != "setProp" || ops[0].Key != "home" || ops[0].Prop != "title" {
		t.Fatalf("want one setProp title, got %+v", ops)
	}
}

func TestDiffInsertRemove(t *testing.T) {
	oldVM := `{"key":"home","type":"page","props":{},"children":[{"key":"a","type":"text","props":{"text":"A"},"children":[]}]}`
	newVM := `{"key":"home","type":"page","props":{},"children":[{"key":"b","type":"text","props":{"text":"B"},"children":[]}]}`
	kinds := opKinds(mustDiff(t, oldVM, newVM))
	if kinds["remove"] != 1 || kinds["insert"] != 1 {
		t.Fatalf("want 1 remove + 1 insert, got %v", kinds)
	}
}

func TestDiffReplaceOnTypeChange(t *testing.T) {
	oldVM := `{"key":"n","type":"text","props":{"text":"x"},"children":[]}`
	newVM := `{"key":"n","type":"header","props":{"text":"x"},"children":[]}`
	ops := mustDiff(t, oldVM, newVM)
	if len(ops) != 1 || ops[0].Op != "replace" {
		t.Fatalf("want one replace, got %+v", ops)
	}
}

func TestDiffMove(t *testing.T) {
	mk := func(order string) string {
		return `{"key":"home","type":"page","props":{},"children":[` + order + `]}`
	}
	a := `{"key":"a","type":"text","props":{"text":"A"},"children":[]}`
	b := `{"key":"b","type":"text","props":{"text":"B"},"children":[]}`
	ops := mustDiff(t, mk(a+","+b), mk(b+","+a))
	found := false
	for _, o := range ops {
		if o.Op == "move" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a move op, got %+v", ops)
	}
}

func TestDiffSetText(t *testing.T) {
	oldVM := `{"key":"n","type":"text","props":{"text":"a"},"children":[]}`
	newVM := `{"key":"n","type":"text","props":{"text":"b"},"children":[]}`
	ops := mustDiff(t, oldVM, newVM)
	found := false
	for _, o := range ops {
		if o.Op == "setText" && o.Value == "b" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want setText b, got %+v", ops)
	}
}

func TestDiffMissingKey(t *testing.T) {
	if _, err := DiffViewModels(`{"type":"x"}`, `{"key":"a","type":"x"}`); err == nil {
		t.Fatal("want error for missing key")
	}
}

func TestDiffOpsJSON(t *testing.T) {
	ops := mustDiff(t,
		`{"key":"h","type":"page","props":{},"children":[]}`,
		`{"key":"h","type":"page","props":{"title":"t"},"children":[]}`)
	data, err := json.Marshal(ops)
	if err != nil || len(data) == 0 {
		t.Fatalf("ops must marshal: %v", err)
	}
}
