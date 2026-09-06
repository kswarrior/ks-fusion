package tools

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kswarrior/ks-fusion/internal/backend"
	"github.com/kswarrior/ks-fusion/internal/frontend"
)

func backendBuiltinNames() []string { return backend.BuiltinNames() }

// Minimal LSP (v2.4): stdio JSON-RPC with initialize, hover, definition, shutdown.
// Enough for VS Code extension (hover/goto-def) + format-on-save via `fusion fmt`.

type lspRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type lspResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
	Method  string `json:"method,omitempty"`
	Params  any    `json:"params,omitempty"`
}

var openDocs = struct {
	mu sync.Mutex
	m  map[string]string
}{m: map[string]string{}}

func setOpenDoc(uri, text string) {
	openDocs.mu.Lock()
	defer openDocs.mu.Unlock()
	openDocs.m[uri] = text
}

func dropOpenDoc(uri string) {
	openDocs.mu.Lock()
	defer openDocs.mu.Unlock()
	delete(openDocs.m, uri)
}

func getOpenDoc(uri string) (string, bool) {
	openDocs.mu.Lock()
	defer openDocs.mu.Unlock()
	s, ok := openDocs.m[uri]
	return s, ok
}

func RunLSP() error {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	respond := func(id any, result any) {
		b, _ := json.Marshal(lspResponse{JSONRPC: "2.0", ID: id, Result: result})
		fmt.Fprintf(out, "Content-Length: %d\r\n\r\n%s", len(b), b)
		out.Flush()
	}
	notify := func(method string, params any) {
		b, _ := json.Marshal(lspResponse{JSONRPC: "2.0", Method: method, Params: params})
		fmt.Fprintf(out, "Content-Length: %d\r\n\r\n%s", len(b), b)
		out.Flush()
	}
	// read LSP framing (Content-Length) or plain lines (for tests)
	reader := bufio.NewReader(os.Stdin)
	for {
		// try header
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil
		}
		line = strings.TrimSpace(line)
		var body []byte
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			n, _ := strconv.Atoi(strings.TrimSpace(line[len("content-length:"):]))
			// consume remaining headers
			for {
				h, _ := reader.ReadString('\n')
				if strings.TrimSpace(h) == "" {
					break
				}
			}
			body = make([]byte, n)
			read := 0
			for read < n {
				m, err := reader.Read(body[read:])
				if err != nil {
					return nil
				}
				read += m
			}
		} else if line == "" {
			continue
		} else {
			body = []byte(line)
		}
		var req lspRequest
		if err := json.Unmarshal(body, &req); err != nil {
			continue
		}
		switch req.Method {
		case "initialize":
			respond(req.ID, map[string]any{"capabilities": map[string]any{
				"textDocumentSync": 1, "hoverProvider": true, "definitionProvider": true,
				"documentFormattingProvider": true, "renameProvider": true,
			}, "serverInfo": map[string]any{"name": "fusion-lsp", "version": "v2.4"}})
		case "initialized":
			// no-op
		case "shutdown":
			respond(req.ID, nil)
			return nil
		case "exit":
			return nil
		case "textDocument/didOpen":
			var p struct {
				TextDocument struct {
					URI  string `json:"uri"`
					Text string `json:"text"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(req.Params, &p)
			setOpenDoc(p.TextDocument.URI, p.TextDocument.Text)
			notify("textDocument/publishDiagnostics", diagnosticsParams(p.TextDocument.URI))
		case "textDocument/didChange":
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
				ContentChanges []struct {
					Text string `json:"text"`
				} `json:"contentChanges"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if len(p.ContentChanges) > 0 {
				setOpenDoc(p.TextDocument.URI, p.ContentChanges[len(p.ContentChanges)-1].Text)
			}
			notify("textDocument/publishDiagnostics", diagnosticsParams(p.TextDocument.URI))
		case "textDocument/didClose":
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(req.Params, &p)
			dropOpenDoc(p.TextDocument.URI)
			notify("textDocument/publishDiagnostics", map[string]any{"uri": p.TextDocument.URI, "diagnostics": []any{}})
		case "textDocument/didSave":
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(req.Params, &p)
			notify("textDocument/publishDiagnostics", diagnosticsParamsSaved(p.TextDocument.URI))
		case "textDocument/hover":
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
				Position struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"position"`
			}
			_ = json.Unmarshal(req.Params, &p)
			respond(req.ID, hoverFor(p.TextDocument.URI, p.Position.Line, p.Position.Character))
		case "textDocument/definition":
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
				Position struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"position"`
			}
			_ = json.Unmarshal(req.Params, &p)
			respond(req.ID, definitionFor(p.TextDocument.URI, p.Position.Line, p.Position.Character))
		case "textDocument/formatting":
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(req.Params, &p)
			respond(req.ID, formattingEdits(p.TextDocument.URI))
		case "textDocument/rename":
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
				Position struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"position"`
				NewName string `json:"newName"`
			}
			_ = json.Unmarshal(req.Params, &p)
			edits, err := renameEdits(p.TextDocument.URI, p.Position.Line, p.Position.Character, p.NewName)
			if err != nil {
				respond(req.ID, map[string]any{"error": err.Error()})
				break
			}
			respond(req.ID, map[string]any{"changes": edits})
		default:
			if req.ID != nil {
				respond(req.ID, nil)
			}
		}
		_ = sc
	}
}

func uriToPath(uri string) string {
	s := strings.TrimPrefix(uri, "file://")
	return filepath.FromSlash(s)
}

func wordAt(path string, line, ch int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if line < 0 || line >= len(lines) {
		return ""
	}
	l := lines[line]
	if ch < 0 || ch > len(l) {
		return ""
	}
	// expand to ident
	s, e := ch, ch
	for s > 0 && isIdentChar(l[s-1]) {
		s--
	}
	for e < len(l) && isIdentChar(l[e]) {
		e++
	}
	return l[s:e]
}

func isIdentChar(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func hoverFor(uri string, line, ch int) any {
	path := uriToPath(uri)
	word := wordAt(path, line, ch)
	if word == "" {
		return nil
	}
	// search func defs in same dir tree
	dir := filepath.Dir(path)
	root := dir
	for {
		if _, err := os.Stat(filepath.Join(root, "fusion.toml")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			root = dir
			break
		}
		root = parent
	}
	files, _ := collectKsFiles(root)
	for _, f := range files {
		prog, err := frontend.ParseFile(f)
		if err != nil {
			continue
		}
		for _, st := range prog.Statements {
			if st.Kind == frontend.StmtFunc && st.Name == word {
				sig := "func " + st.Name + "(" + strings.Join(st.Names, ", ") + ")"
				return map[string]any{"contents": map[string]any{"kind": "markdown", "value": "```ks\n" + sig + "\n```\n" + f + ":" + strconv.Itoa(st.Line)}}
			}
		}
	}
	// builtin?
	for _, n := range backendBuiltinNames() {
		if n == word {
			return map[string]any{"contents": map[string]any{"kind": "markdown", "value": "```ks\nbuiltin " + word + "\n```"}}
		}
	}
	return nil
}

func definitionFor(uri string, line, ch int) any {
	path := uriToPath(uri)
	word := wordAt(path, line, ch)
	if word == "" {
		return nil
	}
	dir := filepath.Dir(path)
	root := dir
	for {
		if _, err := os.Stat(filepath.Join(root, "fusion.toml")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			root = dir
			break
		}
		root = parent
	}
	files, _ := collectKsFiles(root)
	for _, f := range files {
		prog, err := frontend.ParseFile(f)
		if err != nil {
			continue
		}
		for _, st := range prog.Statements {
			if st.Kind == frontend.StmtFunc && st.Name == word {
				return map[string]any{"uri": "file://" + filepath.ToSlash(f), "range": map[string]any{
					"start": map[string]any{"line": 0, "character": 0},
					"end":   map[string]any{"line": 0, "character": 0},
				}}
			}
		}
	}
	return nil
}
