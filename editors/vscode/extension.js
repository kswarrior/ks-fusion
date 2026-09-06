// Minimal VS Code client for `fusion lsp` (stdio JSON-RPC).
// Install: copy this folder to ~/.vscode/extensions/ks-fusion/ (needs node + fusion on PATH),
// or point "ks-fusion.serverPath" at your fusion binary.
const { spawn } = require('child_process');

function activate(context) {
  const vscode = require('vscode');
  const config = vscode.workspace.getConfiguration('ks-fusion');
  const serverPath = config.get('serverPath', 'fusion');
  const server = spawn(serverPath, ['lsp'], { stdio: ['pipe', 'pipe', 'inherit'] });
  server.on('error', (err) => {
    vscode.window.showErrorMessage(`ks-fusion LSP failed to start (${serverPath} lsp): ${err.message}`);
  });
  const send = (msg) => {
    const body = JSON.stringify({ jsonrpc: '2.0', ...msg });
    server.stdin.write(`Content-Length: ${Buffer.byteLength(body)}\r\n\r\n${body}`);
  };
  let buf = Buffer.alloc(0);
  server.stdout.on('data', (chunk) => {
    buf = Buffer.concat([buf, chunk]);
    // Minimal framing: forward complete messages to an output channel for debugging.
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
    }
  });
  const channel = vscode.window.createOutputChannel('ks-fusion LSP');
  send({ id: 1, method: 'initialize', params: { capabilities: {} } });
  context.subscriptions.push({ dispose: () => server.kill() });
}

function deactivate() {}

module.exports = { activate, deactivate };
