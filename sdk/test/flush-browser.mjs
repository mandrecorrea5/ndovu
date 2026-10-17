// Comportamento do flush() do ndovu-browser.js, sem browser.
//
// Roda em Node puro (`npm run check`) — sem runner, sem dependência. Cobre o
// que deixou de ser coberto quando um 401 de ingestão passou a descartar lote
// em silêncio: a decisão reenviar-ou-descartar e o teto da fila.
//
// A ordem dos blocos importa: `warnOnce` é um Set em escopo de módulo, então a
// primeira ocorrência de cada mensagem é a que aparece.

const warned = [];
console.warn = (msg, data) => warned.push({ msg, data });

Object.defineProperty(globalThis, 'navigator', {
  value: { userAgent: 'node-harness', sendBeacon: undefined },
  configurable: true,
  writable: true,
});
globalThis.sessionStorage = { getItem: () => null, setItem: () => {} };
globalThis.window = globalThis;
globalThis.addEventListener = () => {};
globalThis.location = { pathname: '/t', href: 'http://x/t', origin: 'http://x' };
globalThis.document = undefined;

const { createNdovu } = await import(new URL('../src/ndovu-browser.js', import.meta.url).href);

let status = 200;
let thrown = false;
const sent = [];
globalThis.fetch = async (_url, init) => {
  if (thrown) throw new TypeError('Failed to fetch');
  sent.push(...JSON.parse(init.body).events.map((e) => e.eventId));
  return { ok: status < 400, status };
};

function fresh() {
  warned.length = 0;
  sent.length = 0;
  return createNdovu({
    endpoint: 'http://x',
    apiKey: 'ndk_test',
    app: 'harness',
    flushIntervalMs: 1e9,
    maxBatch: 1e6,
    captureGlobals: false,
    captureWebVitals: false,
    breadcrumbs: false,
  });
}

const results = [];
const check = (name, pass, extra = '') => results.push({ name, pass: !!pass, extra });

status = 401;
let sdk = fresh();
sdk.action('a1');
await sdk.flush();
sdk.action('a2');
await sdk.flush();
check('401 descarta sem reenviar o lote', new Set(sent).size === sent.length && sent.length === 2, `sent=${sent.length}`);
const w401 = warned.filter((w) => /recusados \(HTTP 401\)/.test(w.msg));
check('401 avisa exatamente uma vez', w401.length === 1, `warns=${w401.length}`);
check('401 entrega dica acionável', /X-Api-Key/.test(w401[0]?.data?.hint ?? ''), '');

status = 500;
sdk = fresh();
sdk.action('b1');
await sdk.flush();
const retryId = sent[0];
await sdk.flush();
check('500 retém e reenvia o mesmo eventId', sent.length === 2 && sent[1] === retryId, JSON.stringify(sent));
check('500 avisa indisponibilidade', warned.some((w) => /retidos/.test(w.msg)), '');

status = 200;
thrown = true;
sdk = fresh();
sdk.action('c1');
await sdk.flush();
thrown = false;
await sdk.flush();
check('falha de rede retém e reenvia', sent.length === 1, `sent=${sent.length}`);
check('falha de rede não emite warn de status', warned.length === 0, `warns=${warned.length}`);

status = 500;
sdk = fresh();
let capped = false;
for (let cycle = 0; cycle < 12 && !capped; cycle++) {
  for (let i = 0; i < 50; i++) sdk.action(`d${cycle}-${i}`);
  await sdk.flush();
  capped = warned.some((w) => /fila excedeu 400/.test(w.msg));
}
check('fila tem teto em 400 eventos', capped, warned.map((w) => w.msg).join(' | '));
check('teto da fila avisa uma vez', warned.filter((w) => /fila excedeu 400/.test(w.msg)).length === 1, '');

status = 200;
sdk = fresh();
sdk.action('e1');
await sdk.flush();
check('200 envia e não avisa', sent.length === 1 && warned.length === 0, `sent=${sent.length}`);

let failed = 0;
for (const r of results) {
  if (!r.pass) failed++;
  console.log(`${r.pass ? 'ok' : 'FALHOU'}  ${r.name}${!r.pass && r.extra ? '  -> ' + r.extra : ''}`);
}
if (failed) console.error(`\nflush(browser): ${failed} verificação(ões) falharam`);
process.exit(failed ? 1 : 0);
