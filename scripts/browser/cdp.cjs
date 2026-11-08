// Petit pilote réservé aux essais : protocole CDP de Chrome, sans bibliothèque.
// Documentation : https://chromedevtools.github.io/devtools-protocol/
const { spawn, execFileSync } = require('node:child_process');
const fs = require('node:fs/promises');
const path = require('node:path');
const os = require('node:os');
const readline = require('node:readline');

const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
async function until(description, check, timeout = 15000) {
  const deadline = Date.now() + timeout;
  let last;
  do {
    try { const result = await check(); if (result) return result; } catch (err) { last = err; }
    await pause(50);
  } while (Date.now() < deadline);
  throw new Error(`Délai dépassé : ${description}${last ? '\n' + last.message : ''}`);
}

function windowsEnv(name) {
  // Constant names only; no project text is ever passed to a shell.
  if (!/^[A-Z]+$/.test(name)) throw new Error('Nom de variable Windows invalide');
  return execFileSync('powershell.exe', ['-NoProfile', '-NonInteractive', '-Command', `[Console]::Write($env:${name})`], { encoding: 'utf8' }).trim();
}
const wslPath = (p) => execFileSync('wslpath', ['-u', p], { encoding: 'utf8' }).trim();

async function chromeLocation() {
  if (process.env.GLOSE_BROWSER) return process.env.GLOSE_BROWSER;
  const names = process.platform === 'win32'
    ? [path.join(process.env.PROGRAMFILES || '', 'Google/Chrome/Application/chrome.exe'), path.join(process.env.LOCALAPPDATA || '', 'Google/Chrome/Application/chrome.exe')]
    : ['chromium', 'chromium-browser', 'google-chrome', 'google-chrome-stable'].flatMap((name) => (process.env.PATH || '').split(path.delimiter).map((dir) => path.join(dir, name)));
  if (process.platform === 'darwin') names.push('/Applications/Google Chrome.app/Contents/MacOS/Google Chrome');
  for (const name of names) { try { await fs.access(name); return name; } catch (_) {} }
  // Under WSL, use installed Windows Chrome with a WINDOWS-local test profile.
  if (process.platform === 'linux' && /microsoft/i.test(os.release())) {
    for (const variable of ['PROGRAMFILES', 'LOCALAPPDATA']) {
      try {
        const name = wslPath(windowsEnv(variable) + '\\Google\\Chrome\\Application\\chrome.exe');
        await fs.access(name); return name;
      } catch (_) {}
    }
  }
  throw new Error('Chrome/Chromium introuvable. Définissez GLOSE_BROWSER avec son chemin ; aucune installation automatique.');
}

class CDP {
  constructor(ws) {
    this.ws = ws; this.next = 0; this.pending = new Map(); this.listeners = [];
    ws.addEventListener('message', (event) => {
      const message = JSON.parse(event.data);
      if (message.id) {
        const p = this.pending.get(message.id);
        if (!p) return;
        this.pending.delete(message.id); clearTimeout(p.timer);
        if (message.error) p.reject(new Error(`${p.method}: ${message.error.message}`)); else p.resolve(message.result);
      } else for (const listener of this.listeners) listener(message);
    });
    ws.addEventListener('close', () => {
      for (const p of this.pending.values()) { clearTimeout(p.timer); p.reject(new Error(`Connexion CDP fermée : ${p.method}`)); }
      this.pending.clear();
    });
  }
  send(method, params = {}, sessionId) {
    const id = ++this.next;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => { this.pending.delete(id); reject(new Error(`Délai CDP : ${method}`)); }, 20000);
      this.pending.set(id, { resolve, reject, timer, method });
      try { this.ws.send(JSON.stringify({ id, method, params, ...(sessionId ? { sessionId } : {}) })); }
      catch (err) { clearTimeout(timer); this.pending.delete(id); reject(err); }
    });
  }
}

async function openSocket(endpoint, windowsFromWSL) {
  if (windowsFromWSL) {
    const executable = process.env.GLOSE_WINDOWS_NODE || wslPath(windowsEnv('PROGRAMFILES') + '\\nodejs\\node.exe');
    const relay = execFileSync('wslpath', ['-w', path.join(__dirname, 'relay.cjs')], { encoding: 'utf8' }).trim();
    const child = spawn(executable, [relay, endpoint], { stdio: ['pipe', 'pipe', 'pipe'] });
    const transport = new EventTarget();
    transport.send = (data) => child.stdin.write(data + '\n');
    transport.close = () => child.stdin.end();
    const lines = readline.createInterface({ input: child.stdout });
    await new Promise((resolve, reject) => {
      let ready = false;
      const timer = setTimeout(() => { child.kill(); reject(new Error('Délai du relais Windows du navigateur')); }, 10000);
      const fail = (err) => { clearTimeout(timer); reject(new Error('Relais du navigateur : Node.js Windows 22+ requis (GLOSE_WINDOWS_NODE). ' + err.message)); };
      child.on('error', fail);
      let log = '';
      child.stderr.on('data', (b) => { log = (log + b).slice(-2000); });
      child.on('exit', (code) => { clearTimeout(timer); if (!ready || code) fail(new Error(log || 'Relais fermé avant connexion')); transport.dispatchEvent(new Event('close')); });
      lines.on('line', (line) => {
        const message = JSON.parse(line);
        if (message._glose === 'open') { ready = true; clearTimeout(timer); resolve(); }
        else if (message._glose === 'error') fail(new Error('Connexion Windows locale impossible'));
        else transport.dispatchEvent(new MessageEvent('message', { data: line }));
      });
    });
    return transport;
  }
  const ws = new WebSocket(endpoint);
  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => { ws.close(); reject(new Error('Le navigateur ne répond pas sur son port local')); }, 10000);
    ws.addEventListener('open', () => { clearTimeout(timer); resolve(); }, { once: true });
    ws.addEventListener('error', () => { clearTimeout(timer); reject(new Error('Connexion locale au navigateur impossible')); }, { once: true });
  });
  return ws;
}

async function launchChrome(tempRoot) {
  if (Number(process.versions.node.split('.')[0]) < 22 || typeof WebSocket !== 'function') throw new Error('Node.js 22 ou plus récent est requis pour le WebSocket intégré.');
  const executable = await chromeLocation();
  const windowsFromWSL = process.platform !== 'win32' && /\.exe$/i.test(executable);
  const base = windowsFromWSL ? wslPath(windowsEnv('TEMP')) : tempRoot;
  const profile = await fs.mkdtemp(path.join(base, 'glose-browser-'));
  const profileArgument = windowsFromWSL ? execFileSync('wslpath', ['-w', profile], { encoding: 'utf8' }).trim() : profile;
  const child = spawn(executable, [
    '--headless=new', '--remote-debugging-port=0', '--remote-debugging-address=127.0.0.1',
    `--user-data-dir=${profileArgument}`, '--no-first-run', '--no-default-browser-check',
    '--disable-extensions', '--disable-background-networking', '--disable-component-update',
    '--disable-sync', '--disable-default-apps', '--disable-features=Translate,MediaRouter',
    '--metrics-recording-only', '--window-size=1440,1000', 'about:blank',
  ], { stdio: ['ignore', 'ignore', 'pipe'] });
  let log = '', spawnError;
  child.stderr.on('data', (b) => { log = (log + b).slice(-12000); });
  child.on('error', (err) => { spawnError = err; });
  let cdp;
  try {
    const endpoint = await until('démarrage de Chrome isolé', async () => {
      if (spawnError) throw spawnError;
      const [port, suffix] = (await fs.readFile(path.join(profile, 'DevToolsActivePort'), 'utf8')).trim().split(/\r?\n/);
      if (!/^\d+$/.test(port) || !suffix?.startsWith('/devtools/browser/')) return false;
      return `ws://127.0.0.1:${port}${suffix}`;
    });
    const ws = await openSocket(endpoint, windowsFromWSL);
    cdp = new CDP(ws);
    return {
      cdp, version: await cdp.send('Browser.getVersion'),
      async page() { return Page.create(cdp); },
      async close() {
        try { await cdp.send('Browser.close'); } catch (_) {} finally { ws.close(); }
        await until('arrêt du navigateur de test', () => child.exitCode !== null || child.signalCode !== null, 5000).catch(() => child.kill());
        // Only a directory created by this function is removed, never a user's profile.
        await fs.rm(profile, { recursive: true, force: true, maxRetries: 10, retryDelay: 100 }).catch((err) => console.error(`Profil temporaire conservé : ${profile} (${err.message})`));
      },
    };
  } catch (err) {
    child.kill();
    await fs.rm(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 }).catch(() => {});
    throw new Error(`${err.message}\n${log}`);
  }
}

class Page {
  static async create(cdp) {
    const { targetId } = await cdp.send('Target.createTarget', { url: 'about:blank', background: false });
    const { sessionId } = await cdp.send('Target.attachToTarget', { targetId, flatten: true });
    const p = new Page(cdp, targetId, sessionId);
    await p.send('Page.enable'); await p.send('Runtime.enable');
    await p.send('Emulation.setFocusEmulationEnabled', { enabled: true });
    await p.send('Page.bringToFront');
    return p;
  }
  constructor(cdp, target, session) {
    this.cdp = cdp; this.target = target; this.session = session; this.errors = [];
    this.listener = (message) => {
      if (message.sessionId !== session) return;
      if (message.method === 'Runtime.exceptionThrown') this.errors.push(message.params.exceptionDetails.exception?.description || message.params.exceptionDetails.text);
      if (message.method === 'Page.javascriptDialogOpening') {
        this.errors.push('Boîte système inattendue : ' + message.params.message);
        this.send('Page.handleJavaScriptDialog', { accept: false }).catch(() => {});
      }
    };
    cdp.listeners.push(this.listener);
  }
  send(method, params) { return this.cdp.send(method, params, this.session); }
  async evaluate(fn, ...args) {
    const result = await this.send('Runtime.evaluate', {
      expression: `(${fn.toString()})(...${JSON.stringify(args)})`, awaitPromise: true, returnByValue: true, userGesture: true,
    });
    if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description || result.exceptionDetails.text);
    return result.result.value;
  }
  async wait(description, fn, ...args) { return until(description, () => this.evaluate(fn, ...args)); }
  // selector: for a page other than the interface (the settings of the
  // agents), the element that tells it is loaded.
  async navigate(url, selector) {
    const marker = String(Date.now()) + ':' + Math.random();
    await this.evaluate((marker) => { window.__gloseOldDocument = marker; }, marker);
    // A fragment-only navigation would NOT reload the app. A disposable query
    // forces a real document load; Glose removes it with the session fragment.
    const destination = new URL(url);
    destination.searchParams.set('glose-test', marker);
    // WSL may expose a freshly opened Linux port to Windows a little later
    // than it becomes reachable from Linux itself. Wait for network readiness,
    // not for a guessed sleep, and never retry a user action or an assertion.
    await until('accès du navigateur au serveur local', async () => {
      const result = await this.send('Page.navigate', { url: destination.href });
      if (result.errorText) throw new Error(result.errorText);
      return true;
    });
    if (selector) await this.wait('chargement de ' + selector, (marker, selector) => window.__gloseOldDocument !== marker && document.readyState === 'complete' && !!document.querySelector(selector), marker, selector);
    else await this.wait('chargement d’un nouveau document Glose', (marker) => window.__gloseOldDocument !== marker && document.querySelector('#texte')?.getAttribute('aria-busy') === 'false' && !!document.querySelector('#texte span[data-g]'), marker);
    await this.evaluate(() => {
      window.__gloseTest = { events: [], requests: [] };
      for (const type of ['click', 'submit', 'cancel', 'close']) document.addEventListener(type, (e) => {
        window.__gloseTest.events.push({ type, id: e.target.id, label: e.target.getAttribute?.('aria-label'), x: e.clientX, y: e.clientY });
      }, true);
      const original = window.fetch;
      window.fetch = async (...args) => {
        const r = await original(...args);
        const trace = { url: String(args[0]), status: r.status };
        if (!r.ok) {
          trace.body = await r.clone().json().catch(() => null);
          trace.request = args[1]?.body;
        }
        window.__gloseTest.requests.push(trace); return r;
      };
    });
  }
  async click(selector) {
    const pos = await this.evaluate(async (selector) => {
      const element = document.querySelector(selector);
      if (!element || element.disabled) throw new Error('Bouton absent ou désactivé : ' + selector);
      element.scrollIntoView({ block: 'center', behavior: 'instant' });
      // Scrolling can itself reposition the annotation cards on the next
      // frame. Wait for that layout BEFORE awaiting finite CSS animations.
      await new Promise(requestAnimationFrame);
      await new Promise(requestAnimationFrame);
      // Dialogs slide into place: a click at a transient rectangle can hit
      // another button. Await finite ancestor animations, not an arbitrary nap.
      for (let n = element; n; n = n.parentElement) {
        await Promise.all(n.getAnimations().filter((a) => a.effect?.getTiming().iterations !== Infinity).map((a) => a.finished.catch(() => {})));
      }
      await new Promise(requestAnimationFrame);
      const r = element.getBoundingClientRect();
      if (!r.width || !r.height) throw new Error('Élément masqué : ' + selector);
      const hit = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
      if (!hit || !element.contains(hit)) throw new Error('Élément recouvert : ' + selector);
      window.__gloseClick = null;
      document.addEventListener('click', (e) => { window.__gloseClick = { hit: element.contains(e.target), actual: e.target.closest('button')?.getAttribute('aria-label') || e.target.id, expected: selector }; }, { capture: true, once: true });
      return { x: r.x + r.width / 2, y: r.y + r.height / 2 };
    }, selector);
    await this.send('Input.dispatchMouseEvent', { type: 'mouseMoved', ...pos });
    await this.send('Input.dispatchMouseEvent', { type: 'mousePressed', button: 'left', clickCount: 1, ...pos });
    await this.send('Input.dispatchMouseEvent', { type: 'mouseReleased', button: 'left', clickCount: 1, ...pos });
    const click = await this.evaluate(() => window.__gloseClick);
    if (!click?.hit) throw new Error('Clic non reçu par sa cible : ' + JSON.stringify(click));
  }
  async fill(selector, text) {
    await this.evaluate((selector) => {
      const element = document.querySelector(selector); element.focus();
      if (element.isContentEditable) { const r = document.createRange(); r.selectNodeContents(element); const s = getSelection(); s.removeAllRanges(); s.addRange(r); }
      else element.setSelectionRange(0, element.value.length);
    }, selector);
    // A trusted browser input event, including emoji and non-Latin characters.
    await this.send('Input.insertText', { text });
  }
  async key(key, modifiers = 0) {
    const special = { Enter: 13, Escape: 27, Tab: 9, Backspace: 8, ArrowUp: 38, ArrowDown: 40 };
    const code = special[key] || key.toUpperCase().charCodeAt(0);
    const physical = special[key] ? key : /^[0-9]$/.test(key) ? 'Digit' + key : 'Key' + key.toUpperCase();
    const params = { key, code: physical, windowsVirtualKeyCode: code, modifiers };
    await this.send('Input.dispatchKeyEvent', { type: 'keyDown', ...params, ...(!modifiers && key.length === 1 ? { text: key } : {}) });
    await this.send('Input.dispatchKeyEvent', { type: 'keyUp', ...params });
  }
  async close() {
    this.cdp.listeners = this.cdp.listeners.filter((l) => l !== this.listener);
    await this.cdp.send('Target.closeTarget', { targetId: this.target });
  }
}

// The relay and the WSL helpers also serve the Firefox driver (bidi.cjs).
module.exports = { launchChrome, until, pause, openSocket, windowsEnv, wslPath };
