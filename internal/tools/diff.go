package tools

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Keyed view-model diff (v2.5): turns an old and new view-model tree into a
// list of patch ops the browser applies without a reload. Nodes are matched
// by their `key` field; moves are index changes under the same parent.

// PatchOp is one DOM patch. Key addresses the node, Parent/Index place it.
type PatchOp struct {
	Op     string `json:"op"` // setText|setProp|replace|insert|remove|move
	Key    string `json:"key"`
	Parent string `json:"parent,omitempty"`
	Index  int    `json:"index,omitempty"`
	Prop   string `json:"prop,omitempty"`
	Value  any    `json:"value,omitempty"`
}

type vmNode struct {
	Key      string
	Type     string
	Props    map[string]any
	Children []*vmNode
	Raw      map[string]any
}

func toVMNode(v any) (*vmNode, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("view-model node must be a map")
	}
	n := &vmNode{Props: map[string]any{}, Raw: m}
	if k, ok := m["key"].(string); ok {
		n.Key = k
	}
	if t, ok := m["type"].(string); ok {
		n.Type = t
	}
	if p, ok := m["props"].(map[string]any); ok {
		n.Props = p
	}
	if ch, ok := m["children"].([]any); ok {
		for _, c := range ch {
			kid, err := toVMNode(c)
			if err != nil {
				return nil, err
			}
			n.Children = append(n.Children, kid)
		}
	}
	if n.Key == "" {
		return nil, fmt.Errorf("view-model node missing key")
	}
	return n, nil
}

func indexTree(n *vmNode, parent string, out map[string]*vmNode, par map[string]string) {
	out[n.Key] = n
	par[n.Key] = parent
	for _, c := range n.Children {
		indexTree(c, n.Key, out, par)
	}
}

func jsonEqual(a, b any) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}

// DiffViewModels diffs two view-model JSON documents by node key.
func DiffViewModels(oldJSON, newJSON string) ([]PatchOp, error) {
	var ov, nv any
	if err := json.Unmarshal([]byte(oldJSON), &ov); err != nil {
		return nil, fmt.Errorf("old view-model: %w", err)
	}
	if err := json.Unmarshal([]byte(newJSON), &nv); err != nil {
		return nil, fmt.Errorf("new view-model: %w", err)
	}
	oldRoot, err := toVMNode(ov)
	if err != nil {
		return nil, fmt.Errorf("old view-model: %w", err)
	}
	newRoot, err := toVMNode(nv)
	if err != nil {
		return nil, fmt.Errorf("new view-model: %w", err)
	}
	oldByKey := map[string]*vmNode{}
	oldParent := map[string]string{}
	indexTree(oldRoot, "", oldByKey, oldParent)
	newByKey := map[string]*vmNode{}
	newParent := map[string]string{}
	indexTree(newRoot, "", newByKey, newParent)

	var ops []PatchOp
	// removals first (children before parents: deeper keys first for stability)
	var removed []string
	for k := range oldByKey {
		if _, ok := newByKey[k]; !ok {
			removed = append(removed, k)
		}
	}
	sort.Slice(removed, func(i, j int) bool { return removed[i] > removed[j] })
	for _, k := range removed {
		ops = append(ops, PatchOp{Op: "remove", Key: k, Parent: oldParent[k]})
	}
	// walk new tree in document order
	var walk func(n *vmNode, parent string, index int)
	walk = func(n *vmNode, parent string, index int) {
		old, ok := oldByKey[n.Key]
		if !ok {
			ops = append(ops, PatchOp{Op: "insert", Key: n.Key, Parent: parent, Index: index, Value: n.Raw})
			// children of an inserted subtree ride along; still walk them so
			// nested inserts are explicit is unnecessary — skip descent.
			return
		}
		if old.Type != n.Type {
			ops = append(ops, PatchOp{Op: "replace", Key: n.Key, Value: n.Raw})
			return
		}
		// props: changed/added
		for pk, pv := range n.Props {
			if ov, ok := old.Props[pk]; !ok || !jsonEqual(ov, pv) {
				ops = append(ops, PatchOp{Op: "setProp", Key: n.Key, Prop: pk, Value: pv})
			}
		}
		for pk := range old.Props {
			if _, ok := n.Props[pk]; !ok {
				ops = append(ops, PatchOp{Op: "setProp", Key: n.Key, Prop: pk, Value: nil})
			}
		}
		// text: props.text is the text channel
		if ot, ok := old.Props["text"].(string); ok {
			if nt, ok := n.Props["text"].(string); ok && ot != nt {
				ops = append(ops, PatchOp{Op: "setText", Key: n.Key, Value: nt})
			}
		} else if nt, ok := n.Props["text"].(string); ok && nt != "" {
			ops = append(ops, PatchOp{Op: "setText", Key: n.Key, Value: nt})
		}
		// moves: same parent, different index among keyed siblings
		if oldParent[n.Key] == parent {
			oi := childIndex(oldByKey[parent], n.Key)
			if oi >= 0 && oi != index {
				ops = append(ops, PatchOp{Op: "move", Key: n.Key, Parent: parent, Index: index})
			}
		} else if parent != oldParent[n.Key] {
			ops = append(ops, PatchOp{Op: "move", Key: n.Key, Parent: parent, Index: index})
		}
		for i, c := range n.Children {
			walk(c, n.Key, i)
		}
	}
	walk(newRoot, "", 0)
	if ops == nil {
		ops = []PatchOp{}
	}
	return ops, nil
}

func childIndex(n *vmNode, key string) int {
	if n == nil {
		return -1
	}
	for i, c := range n.Children {
		if c.Key == key {
			return i
		}
	}
	return -1
}
