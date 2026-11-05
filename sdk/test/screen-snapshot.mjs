// Modo captureSnapshots: 'screen' — snapshot de DOM em page_view() e error().
//
// Roda em Node puro (npm run check), sem browser real: o DOM é um stub com o
// mínimo que captureDOM() usa. O que importa aqui é o contrato de rede:
// page_view e erro geram POST /v1/snapshots com o mesmo eventId do evento.

const warned = [];
console.warn = (msg, data) => warned.push({ msg, data });

Object.defineProperty(globalThis, 'navigator', {
  value: { userAgent: 'node-harness', sendBeacon: undefined },
  configurable: true,
  writable: true,
});
globalThis.window = globalThis;
globalThis.innerWidth = 1024;
globalThis.innerHeight = 768;
globalThis.sessionStorage = { getItem: () => null, setItem: () => {} };
globalThis.addEventListener = () => {};
globalThis.location = { pathname: '/t', href: 'http://x/t', origin: 'http://x' };
globalThis.requestAnimationFrame = (cb) => {
  cb();
  return 1;
};

const DEFAULT_HTML = '<html><body><h1>tela</h1></body></html>';
let fakeHtml = DEFAULT_HTML;
const clone = {
  querySelectorAll: () => [],
  get outerHTML() {
    return fakeHtml;
  },
};
globalThis.document = {
  documentElement: { cloneNode: () => clone },
};

const events = [];
const snapshots = [];
globalThis.fetch = async (url, init) => {
  const body = JSON.parse(init.body);
  if (String(url).endsWith('/v1/snapshots')) snapshots.push({ ...body, keepalive: init.keepalive });
  else events.push(...body.events);
  return { ok: true, status: 202 };
};

const { createNdovu } = await import(new URL('../src/ndovu-browser.js', import.meta.url).href);

function fresh(captureSnapshots) {
  events.length = 0;
  snapshots.length = 0;
  warned.length = 0;
  return createNdovu({
    endpoint: 'http://x',
    apiKey: 'ndk_test',
    app: 'harness',
    flushIntervalMs: 1e9,
    maxBatch: 1e6,
    captureGlobals: false,
    captureWebVitals: false,
    breadcrumbs: false,
    captureSnapshots,
  });
}

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
const results = [];
const check = (name, pass, extra = '') => results.push({ name, pass: !!pass, extra });

// 'screen': pageView e error geram snapshot, cada um com o eventId do evento.
let sdk = fresh('screen');
sdk.pageView('/faturas');
await sdk.flush();
await tick();
check('screen: pageView gera 1 snapshot', snapshots.length === 1, `snapshots=${snapshots.length}`);
check(
  'screen: snapshot usa o eventId do page_view',
  snapshots[0]?.eventId === events[0]?.eventId,
  `snapshot=${snapshots[0]?.eventId} event=${events[0]?.eventId}`,
);
check('screen: snapshot carrega o HTML do DOM', /<h1>tela<\/h1>/.test(snapshots[0]?.html ?? ''), '');
check('screen: snapshot carrega viewport', snapshots[0]?.viewportW === 1024, '');

sdk.error('boom', { message: 'falhou' });
await tick();
check('screen: error também gera snapshot', snapshots.length === 2, `snapshots=${snapshots.length}`);
check('screen: snapshot pequeno usa keepalive', snapshots[0]?.keepalive === true, '');

// Snapshot grande não cabe no teto de ~64 KB do keepalive: precisa cair para
// fetch normal, senão o browser rejeita antes de o request sair.
fakeHtml = '<html><body>' + 'x'.repeat(70_000) + '</body></html>';
sdk = fresh('screen');
sdk.pageView('/grande');
await tick();
check('screen: snapshot grande não usa keepalive', snapshots[0]?.keepalive === false, '');
fakeHtml = DEFAULT_HTML;

// true/'error': só erro. pageView não deve chamar /v1/snapshots.
sdk = fresh(true);
sdk.pageView('/home');
await sdk.flush();
await tick();
check('true: pageView não gera snapshot', snapshots.length === 0, `snapshots=${snapshots.length}`);
sdk.error('boom');
await tick();
check('true: error gera snapshot', snapshots.length === 1, `snapshots=${snapshots.length}`);

// false/'off': nenhum snapshot, nem no erro.
for (const mode of [false, 'off']) {
  sdk = fresh(mode);
  sdk.pageView('/home');
  sdk.error('boom');
  await tick();
  check(`${mode}: nenhum snapshot`, snapshots.length === 0, `snapshots=${snapshots.length}`);
}

let failed = 0;
for (const r of results) {
  if (!r.pass) failed++;
  console.log(`${r.pass ? 'ok' : 'FALHOU'}  ${r.name}${!r.pass && r.extra ? '  -> ' + r.extra : ''}`);
}
if (failed) console.error(`\nscreen-snapshot: ${failed} verificação(ões) falharam`);
process.exit(failed ? 1 : 0);
