// Under WSL, the debugging port of a Windows browser (Chrome CDP or Firefox
// BiDi) stays on the Windows side. This relay only carries the JSON messages
// of the test browser over stdio.
const readline = require('node:readline');
const endpoint = new URL(process.argv[2]);
if (endpoint.protocol !== 'ws:' || endpoint.hostname !== '127.0.0.1') throw new Error('Relais exclusivement local');
if (Number(process.versions.node.split('.')[0]) < 22 || typeof WebSocket !== 'function') throw new Error('Node.js Windows 22 ou plus récent requis');
const ws = new WebSocket(endpoint);
const lines = readline.createInterface({ input: process.stdin });
const output = (v) => process.stdout.write(JSON.stringify(v) + '\n');
ws.addEventListener('open', () => output({ _glose: 'open' }));
ws.addEventListener('message', (event) => process.stdout.write(event.data + '\n'));
ws.addEventListener('error', () => { output({ _glose: 'error' }); ws.close(); });
ws.addEventListener('close', () => { lines.close(); process.stdin.destroy(); });
lines.on('line', (line) => ws.send(line));
lines.on('close', () => ws.close());
