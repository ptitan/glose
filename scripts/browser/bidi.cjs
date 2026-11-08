// Small test-only driver: Firefox through WebDriver BiDi, without a library.
// Specification: https://w3c.github.io/webdriver-bidi/
// It offers the subset of the Chrome driver (cdp.cjs) that the Firefox
// scenarios need: evaluate, wait, navigate. Messages to the user stay in
// French, like the rest of the test suite.
const { spawn, execFileSync } = require('node:child_process');
const fs = require('node:fs/promises');
const path = require('node:path');
const os = require('node:os');
const { until, openSocket, windowsEnv, wslPath } = require('./cdp.cjs');

const isWSL = () => process.platform === 'linux' && /microsoft/i.test(os.release());

// GLOSE_FIREFOX=0 skips the Firefox scenarios on purpose (checked by the
// caller); a missing Firefox otherwise fails, it never passes silently.
async function firefoxLocation() {
  if (process.env.GLOSE_FIREFOX) return process.env.GLOSE_FIREFOX;
  const names = process.platform === 'win32'
    ? [path.join(process.env.PROGRAMFILES || '', 'Mozilla Firefox/firefox.exe')]
    : (process.env.PATH || '').split(path.delimiter).map((dir) => path.join(dir, 'firefox'));
  if (process.platform === 'darwin') names.push('/Applications/Firefox.app/Contents/MacOS/firefox');
  for (const name of names) { try { await fs.access(name); return name; } catch (_) {} }
  if (isWSL()) {
    try {
      const name = wslPath(windowsEnv('PROGRAMFILES') + '\\Mozilla Firefox\\firefox.exe');
      await fs.access(name); return name;
    } catch (_) {}
  }
  throw new Error('Firefox introuvable. Définissez GLOSE_FIREFOX avec son chemin, ou GLOSE_FIREFOX=0 pour sauter les essais Firefox ; aucune installation automatique.');
}

class BiDi {
  constructor(ws) {
    this.ws = ws; this.next = 0; this.pending = new Map(); this.listeners = [];
    ws.addEventListener('message', (event) => {
      const message = JSON.parse(event.data);
      if (message.id !== undefined) {
        const p = this.pending.get(message.id);
        if (!p) return;
        this.pending.delete(message.id); clearTimeout(p.timer);
        if (message.type === 'error') p.reject(new Error(`${p.method}: ${message.error} ${message.message}`)); else p.resolve(message.result);
      } else if (message.type === 'event') for (const listener of this.listeners) listener(message);
    });
    ws.addEventListener('close', () => {
      for (const p of this.pending.values()) { clearTimeout(p.timer); p.reject(new Error(`Connexion BiDi fermée : ${p.method}`)); }
      this.pending.clear();
    });
  }
  send(method, params = {}) {
    const id = ++this.next;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => { this.pending.delete(id); reject(new Error(`Délai BiDi : ${method}`)); }, 20000);
      this.pending.set(id, { resolve, reject, timer, method });
      try { this.ws.send(JSON.stringify({ id, method, params })); }
      catch (err) { clearTimeout(timer); this.pending.delete(id); reject(err); }
    });
  }
}

async function launchFirefox(tempRoot) {
  if (Number(process.versions.node.split('.')[0]) < 22 || typeof WebSocket !== 'function') throw new Error('Node.js 22 ou plus récent est requis pour le WebSocket intégré.');
  const executable = await firefoxLocation();
  const windowsFromWSL = process.platform !== 'win32' && /\.exe$/i.test(executable);
  const base = windowsFromWSL ? wslPath(windowsEnv('TEMP')) : tempRoot;
  const profile = await fs.mkdtemp(path.join(base, 'glose-firefox-'));
  const profileArgument = windowsFromWSL ? execFileSync('wslpath', ['-w', profile], { encoding: 'utf8' }).trim() : profile;
  // Port 0: Firefox picks a free local port and prints it on stderr. When the
  // remote agent is on, Firefox applies its recommended preferences, which
  // turn off updates and most background requests.
  const child = spawn(executable, [
    '--headless', '--remote-debugging-port=0', '--profile', profileArgument, '--no-remote',
    '--width=1440', '--height=1000', 'about:blank',
  ], { stdio: ['ignore', 'ignore', 'pipe'] });
  let log = '', spawnError;
  child.stderr.on('data', (b) => { log = (log + b).slice(-12000); });
  child.on('error', (err) => { spawnError = err; });
  const removeProfile = async () => {
    // Only the directory created above is removed, never a user's profile.
    // Windows releases the profile files a little after the process exits.
    await fs.rm(profile, { recursive: true, force: true, maxRetries: 20, retryDelay: 250 })
      .catch((err) => console.error(`Profil temporaire conservé : ${profile} (${err.message})`));
  };
  let bidi;
  try {
    const endpoint = await until('démarrage de Firefox isolé', () => {
      if (spawnError) throw spawnError;
      const m = /WebDriver BiDi listening on (ws:\/\/127\.0\.0\.1:\d+)/.exec(log);
      return m && m[1] + '/session';
    }, 30000);
    const ws = await openSocket(endpoint, windowsFromWSL);
    bidi = new BiDi(ws);
    const session = await bidi.send('session.new', { capabilities: {} });
    await bidi.send('session.subscribe', { events: ['log.entryAdded', 'browsingContext.userPromptOpened'] });
    return {
      bidi, version: { product: `Firefox/${session.capabilities.browserVersion}` },
      async page() { return Page.create(bidi); },
      async close() {
        // browser.close ends the session and quits Firefox; the WSL interop
        // process follows it.
        try { await bidi.send('browser.close'); } catch (_) {} finally { ws.close(); }
        await until('arrêt du navigateur de test', () => child.exitCode !== null || child.signalCode !== null, 10000).catch(() => child.kill());
        await removeProfile();
      },
    };
  } catch (err) {
    child.kill();
    await removeProfile();
    throw new Error(`${err.message}\n${log}`);
  }
}

class Page {
  static async create(bidi) {
    const { context } = await bidi.send('browsingContext.create', { type: 'tab' });
    return new Page(bidi, context);
  }
  constructor(bidi, context) {
    this.bidi = bidi; this.context = context; this.errors = [];
    this.listener = (message) => {
      const p = message.params;
      if (message.method === 'log.entryAdded' && p.type === 'javascript' && p.level === 'error' && p.source?.context === context) this.errors.push(p.text);
      if (message.method === 'browsingContext.userPromptOpened' && p.context === context) {
        this.errors.push('Boîte système inattendue : ' + p.message);
        this.send('browsingContext.handleUserPrompt', { context, accept: false }).catch(() => {});
      }
    };
    bidi.listeners.push(this.listener);
  }
  send(method, params) { return this.bidi.send(method, params); }
  // Same contract as cdp.cjs: fn runs in the page with JSON arguments, and
  // its (awaited) result comes back through JSON.
  async evaluate(fn, ...args) {
    const result = await this.send('script.evaluate', {
      expression: `(async () => JSON.stringify(await (${fn.toString()})(...${JSON.stringify(args)})))()`,
      target: { context: this.context }, awaitPromise: true, userActivation: true,
    });
    if (result.type === 'exception') throw new Error(result.exceptionDetails.text);
    return result.result.type === 'string' ? JSON.parse(result.result.value) : undefined;
  }
  async wait(description, fn, ...args) { return until(description, () => this.evaluate(fn, ...args)); }
  async navigate(url) {
    const marker = String(Date.now()) + ':' + Math.random();
    await this.evaluate((marker) => { window.__gloseOldDocument = marker; }, marker);
    // A disposable query forces a real document load (see cdp.cjs), and the
    // first access to a new WSL port may come a little late on Windows.
    const destination = new URL(url);
    destination.searchParams.set('glose-test', marker);
    await until('accès du navigateur au serveur local', async () => {
      await this.send('browsingContext.navigate', { context: this.context, url: destination.href, wait: 'complete' });
      return true;
    });
    await this.wait('chargement d’un nouveau document Glose', (marker) => window.__gloseOldDocument !== marker && document.querySelector('#texte')?.getAttribute('aria-busy') === 'false' && !!document.querySelector('#texte span[data-g]'), marker);
  }
  async close() {
    this.bidi.listeners = this.bidi.listeners.filter((l) => l !== this.listener);
    await this.send('browsingContext.close', { context: this.context });
  }
}

module.exports = { launchFirefox };
