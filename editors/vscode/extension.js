// VS Code client for `fusion lsp` (stdio JSON-RPC, v2.6 full LSP).
// Install: copy this folder to ~/.vscode/extensions/ks-fusion/ (needs node + fusion on PATH),
// or point "ks-fusion.serverPath" at your fusion binary.
// Features: hover, goto-def, rename, diagnostics (parse+vet), formatting, completion
// (builtins + keywords + workspace funcs via `textDocument/completion`),
// plus `fusion debug` breakpoints via the ks-fusion debugger type.
const { spawn } = require('child_process');

function activate(context) {
  const vscode = require('vscode');
  const config = vscode.workspace.getConfiguration('ks-fusion');
  const serverPath = config.get('serverPath', 'fusion');
  const server = spawn(serverPath, ['lsp'], { stdio: ['pipe', 'pipe', 'inherit'] });
  server.on('error', (err) => {
    vscode.window.showErrorMessage(`ks-fusion LSP failed to start (${serverPath} lsp): ${err.message}`);
  });
  const channel = vscode.window.createOutputChannel('ks-fusion LSP');
  const pending = new Map();
  let nextId = 100;
  const send = (msg) => {
    const body = JSON.stringify({ jsonrpc: '2.0', ...msg });
    server.stdin.write(`Content-Length: ${Buffer.byteLength(body)}\r\n\r\n${body}`);
  };
  let buf = Buffer.alloc(0);
  server.stdout.on('data', (chunk) => {
    buf = Buffer.concat([buf, chunk]);
    for (;;) {
      const str = buf.toString('utf8');
      const m = str.match(/Content-Length:\s*(\d+)\r\n\r\n/);
      if (!m) break;
      const n = parseInt(m[1], 10);
      const start = m.index + m[0].length;
      if (buf.length < start + n) break;
      const body = buf.slice(start, start + n).toString('utf8');
      buf = buf.slice(start + n);
      channel.appendLine(body);
      try {
        const msg = JSON.parse(body);
        // publishDiagnostics -> VS Code diagnostics collection
        if (msg.method === 'textDocument/publishDiagnostics' && msg.params) {
          const uri = vscode.Uri.parse(msg.params.uri);
          const diags = (msg.params.diagnostics || []).map((d) => {
            const s = new vscode.Position(d.range.start.line, d.range.start.character);
            const e = new vscode.Position(d.range.end.line, d.range.end.character);
            const sev = d.severity === 1 ? vscode.DiagnosticSeverity.Error : vscode.DiagnosticSeverity.Warning;
            const diag = new vscode.Diagnostic(new vscode.Range(s, e), d.message, sev);
            diag.source = d.source || 'fusion';
            return diag;
          });
          diagCollection.set(uri, diags);
        }
        if (msg.id && pending.has(msg.id)) {
          pending.get(msg.id)(msg.result);
          pending.delete(msg.id);
        }
      } catch (e) { /* ignore malformed frames */ }
    }
  });
  const diagCollection = vscode.languages.createDiagnosticCollection('ks-fusion');
  context.subscriptions.push(diagCollection);

  // document sync + diagnostics
  const syncDoc = (doc) => {
    if (doc.languageId !== 'ks') return;
    send({ method: 'textDocument/didOpen', params: { textDocument: { uri: doc.uri.toString(), text: doc.getText() } } });
  };
  vscode.workspace.textDocuments.forEach(syncDoc);
  context.subscriptions.push(vscode.workspace.onDidOpenTextDocument(syncDoc));
  context.subscriptions.push(vscode.workspace.onDidChangeTextDocument((e) => {
    if (e.document.languageId !== 'ks') return;
    send({ method: 'textDocument/didChange', params: { textDocument: { uri: e.document.uri.toString() }, contentChanges: [{ text: e.document.getText() }] } });
  }));
  context.subscriptions.push(vscode.workspace.onDidCloseTextDocument((doc) => {
    if (doc.languageId !== 'ks') return;
    send({ method: 'textDocument/didClose', params: { textDocument: { uri: doc.uri.toString() } } });
    diagCollection.delete(doc.uri);
  }));

  // hover
  context.subscriptions.push(vscode.languages.registerHoverProvider('ks', {
    provideHover(doc, pos) {
      const id = nextId++;
      return new Promise((resolve) => {
        pending.set(id, (result) => {
          if (result && result.contents) resolve(new vscode.Hover(new vscode.MarkdownString(result.contents.value)));
          else resolve(null);
        });
        send({ id, method: 'textDocument/hover', params: { textDocument: { uri: doc.uri.toString() }, position: { line: pos.line, character: pos.character } } });
        setTimeout(() => { if (pending.has(id)) { pending.delete(id); resolve(null); } }, 2000);
      });
    }
  }));

  // goto-def
  context.subscriptions.push(vscode.languages.registerDefinitionProvider('ks', {
    provideDefinition(doc, pos) {
      const id = nextId++;
      return new Promise((resolve) => {
        pending.set(id, (result) => {
          if (result && result.uri) {
            const uri = vscode.Uri.parse(result.uri);
            const p = new vscode.Position(result.range.start.line, result.range.start.character);
            resolve(new vscode.Location(uri, p));
          } else resolve(null);
        });
        send({ id, method: 'textDocument/definition', params: { textDocument: { uri: doc.uri.toString() }, position: { line: pos.line, character: pos.character } } });
        setTimeout(() => { if (pending.has(id)) { pending.delete(id); resolve(null); } }, 2000);
      });
    }
  }));

  // rename
  context.subscriptions.push(vscode.languages.registerRenameProvider('ks', {
    provideRenameEdits(doc, pos, newName) {
      const id = nextId++;
      return new Promise((resolve, reject) => {
        pending.set(id, (result) => {
          try {
            if (result && result.changes) {
              const edit = new vscode.WorkspaceEdit();
              for (const [uri, edits] of Object.entries(result.changes)) {
                for (const e of edits) {
                  const s = new vscode.Position(e.range.start.line, e.range.start.character);
                  const en = new vscode.Position(e.range.end.line, e.range.end.character);
                  edit.replace(vscode.Uri.parse(uri), new vscode.Range(s, en), e.newText);
                }
              }
              resolve(edit);
            } else resolve(null);
          } catch (e) { reject(e); }
        });
        send({ id, method: 'textDocument/rename', params: { textDocument: { uri: doc.uri.toString() }, position: { line: pos.line, character: pos.character }, newName } });
        setTimeout(() => { if (pending.has(id)) { pending.delete(id); resolve(null); } }, 2000);
      });
    }
  }));

  // formatting (fusion fmt via LSP)
  context.subscriptions.push(vscode.languages.registerDocumentFormattingEditProvider('ks', {
    provideDocumentFormattingEdits(doc) {
      const id = nextId++;
      return new Promise((resolve) => {
        pending.set(id, (result) => {
          const edits = (result || []).map((e) => {
            const s = new vscode.Position(e.range.start.line, e.range.start.character);
            const en = new vscode.Position(e.range.end.line, e.range.end.character);
            return new vscode.TextEdit(new vscode.Range(s, en), e.newText);
          });
          resolve(edits);
        });
        send({ id, method: 'textDocument/formatting', params: { textDocument: { uri: doc.uri.toString() } } });
        setTimeout(() => { if (pending.has(id)) { pending.delete(id); resolve([]); } }, 2000);
      });
    }
  }));

  // completion (builtins + keywords + workspace funcs/structs/enums)
  context.subscriptions.push(vscode.languages.registerCompletionItemProvider('ks', {
    provideCompletionItems(doc, pos) {
      const id = nextId++;
      return new Promise((resolve) => {
        pending.set(id, (result) => {
          const items = ((result && result.items) || []).map((e) => {
            const kind = e.kind === 14 ? vscode.CompletionItemKind.Keyword : vscode.CompletionItemKind.Function;
            const item = new vscode.CompletionItem(e.label, kind);
            item.detail = e.detail || '';
            return item;
          });
          resolve(items);
        });
        send({ id, method: 'textDocument/completion', params: { textDocument: { uri: doc.uri.toString() }, position: { line: pos.line, character: pos.character } } });
        setTimeout(() => { if (pending.has(id)) { pending.delete(id); resolve([]); } }, 2000);
      });
    }
  }, '.', '_'));

  send({ id: 1, method: 'initialize', params: { capabilities: {} } });
  context.subscriptions.push({ dispose: () => { try { server.kill(); } catch (e) {} } });
}

function deactivate() {}

module.exports = { activate, deactivate };
