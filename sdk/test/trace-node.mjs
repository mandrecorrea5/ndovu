// Propagação W3C traceparent no ndovu-node.js, sem Express: o middleware é
// chamado com req/res falsos e o envio é interceptado no fetch.

import { EventEmitter } from 'node:events';

const { createNdovu, parseTraceparent, formatTraceparent } = await import(
  new URL('../src/ndovu-node.js', import.meta.url).href
);

const sent = [];
globalThis.fetch = async (_url, init) => {
  sent.push(JSON.parse(init.body));
  return { ok: true, status: 202 };
};

const results = [];
const check = (name, pass, extra = '') => results.push({ name, pass: !!pass, extra });

const ndovu = createNdovu({ endpoint: 'http://x', apiKey: 'ndk_test', app: 'harness-node', flushIntervalMs: 1e9, maxBatch: 1e6 });
const events = () => sent.flatMap((env) => env.events.map((e) => ({ ...e, session: env.session })));

function request(headers, handler) {
  const req = { method: 'POST', path: '/filiacao', originalUrl: '/filiacao', headers };
  const res = Object.assign(new EventEmitter(), { statusCode: 201, statusMessage: 'Created' });
  ndovu.middleware()(req, res, () => {
    handler?.();
    res.emit('finish');
  });
}

// parse / format
const TRACE = '0af7651916cd43dd8448eb211c80319c';
const SPAN = 'b7ad6b7169203331';
const header = `00-${TRACE}-${SPAN}-01`;
check('parse: header válido', parseTraceparent(header)?.traceId === TRACE && parseTraceparent(header).spanId === SPAN);
check('parse: maiúsculas aceitas', parseTraceparent(header.toUpperCase())?.traceId === TRACE);
check('parse: flag não amostrado', parseTraceparent(`00-${TRACE}-${SPAN}-00`)?.sampled === false);
check('parse: traceId zerado rejeitado', parseTraceparent(`00-${'0'.repeat(32)}-${SPAN}-01`) === null);
check('parse: spanId zerado rejeitado', parseTraceparent(`00-${TRACE}-${'0'.repeat(16)}-01`) === null);
check('parse: versão ff rejeitada', parseTraceparent(`ff-${TRACE}-${SPAN}-01`) === null);
check('parse: lixo rejeitado', parseTraceparent('abc') === null && parseTraceparent(undefined) === null);
check('format: ida e volta', formatTraceparent(parseTraceparent(header)) === header);

// request com traceparent do browser
let outgoing;
request({ traceparent: header, 'x-ndovu-session-id': 'sess-1' }, () => {
  ndovu.action('filiou', { feature: 'filiacao' });
  outgoing = parseTraceparent(ndovu.traceparent());
});
await ndovu.flush();
const http = events().find((e) => e.type === 'http_request');
const action = events().find((e) => e.name === 'filiou');
check('http_request herda traceId do browser', http?.trace?.traceId === TRACE, JSON.stringify(http?.trace));
check('http_request é filho do span do browser', http?.trace?.parentSpanId === SPAN);
check('http_request tem span próprio', /^[0-9a-f]{16}$/.test(http?.trace?.spanId ?? '') && http.trace.spanId !== SPAN);
check('evento manual herda traceId', action?.trace?.traceId === TRACE);
check('evento manual é filho do span do request', action?.trace?.parentSpanId === http?.trace?.spanId);
check('evento manual herda sessão do request', action?.session?.sessionId === 'sess-1');
check('traceparent() de saída continua o trace', outgoing?.traceId === TRACE && outgoing.spanId === http?.trace?.spanId);

// request sem header abre trace novo, sem pai
sent.length = 0;
request({});
await ndovu.flush();
const root = events()[0];
check('sem header: trace novo', /^[0-9a-f]{32}$/.test(root?.trace?.traceId ?? '') && root.trace.traceId !== TRACE);
check('sem header: sem parentSpanId', root?.trace?.parentSpanId === undefined);

// header inválido é ignorado, não propagado
sent.length = 0;
request({ traceparent: '00-xyz-abc-01' });
await ndovu.flush();
check('header inválido: trace novo', /^[0-9a-f]{32}$/.test(events()[0]?.trace?.traceId ?? ''));

// fora de request: comportamento antigo
sent.length = 0;
ndovu.action('job_noturno');
await ndovu.flush();
check('fora de request: sem trace', events()[0]?.trace === undefined);
check('fora de request: traceparent() undefined', ndovu.traceparent() === undefined);

// trace explícito vence o contexto
sent.length = 0;
request({ traceparent: header }, () => ndovu.custom('manual', { trace: { traceId: 'f'.repeat(32), spanId: 'e'.repeat(16) } }));
await ndovu.flush();
check('trace explícito vence o contexto', events().find((e) => e.name === 'manual')?.trace?.traceId === 'f'.repeat(32));

const failed = results.filter((r) => !r.pass);
for (const r of results) console.log(`${r.pass ? 'ok  ' : 'FAIL'} ${r.name}${r.pass ? '' : ' ' + r.extra}`);
if (failed.length) process.exit(1);
console.log(`\n${results.length} verificações ok`);
