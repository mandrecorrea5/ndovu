# ndovu-sdk

SDKs de referência do contrato v1 do Ndovu e ferramenta de carga de exemplo.
**Único repositório que não vira imagem nem deploy** — publica pacote npm.

> Este README é o modelo para o root do repositório `ndovu-sdk`.
> Origem: `sdk/` e `tools/seed/` do monorepo Ndovu.

## O que este repositório é

Conveniência, não obrigação. O que integra um frontend ao Ndovu é o **contrato
v1** — qualquer tecnologia que faça `POST /v1/events` com `X-Api-Key` e o JSON
especificado está integrada, com ou sem SDK. Estes pacotes existem para que o
time de frontend não reescreva batching, retry, redação de dados sensíveis e
captura de erro global em cada projeto.

| Pacote | Alvo | Recursos específicos |
|--------|------|----------------------|
| `ndovu-browser` | Web (qualquer framework) | `window.onerror`, `unhandledrejection`, Web Vitals (LCP, CLS, INP, FID, TTFB, FCP), breadcrumbs, instrumentação de `fetch` |
| `ndovu-next` | Next.js | Integração com App Router e server components |
| `ndovu-node` | Backend Node | Propagação W3C trace context |
| `ndovu-react-native` | Mobile | Ciclo de vida do app, sem APIs de DOM |

Todos com dependências zero e a mesma superfície: `pageView`, `action`,
`error`, `http`, com `userId` + `sessionId` amarrando o rastro.

Redação de dados sensíveis acontece **no cliente, antes do envio** — a regex
de `REDACT_KEYS` cobre senha, token, secret, authorization, cvv e cartão.
Alterar essa lista é mudança de segurança, não de conveniência: revise com o
mesmo rigor de uma mudança no backend.

## Estrutura

```
src/
  ndovu-browser.js
  ndovu-next.js
  ndovu-node.js
  ndovu-react-native.js
tools/seed/            gerador de sessões realistas (login, 2ª via, fatura, PIX, erros)
test/contract/         validação contra o openapi.yaml do ndovu-backend
```

## Uso

```js
import { createNdovu } from '@ndovu/sdk/browser';

const ndovu = createNdovu({
  endpoint: 'https://ingest.ndovu.<domínio>',
  apiKey: '<chave do app, gerada na tela Chaves de API>',
  app: 'portal-cliente',
  getUserId: () => window.currentUserId ?? null,
  captureGlobals: true,
  captureWebVitals: true,
  breadcrumbs: true,
});

ndovu.pageView('/faturas');
ndovu.action('clicou_segunda_via', { feature: 'faturas' });
ndovu.instrumentFetch();
```

A chave de API é **por app emissor**, gerada e revogada no backoffice. Ela vai
para o bundle do frontend, então é pública por natureza: o controle real é a
revogação imediata pela tela de Chaves de API (propagação em até
`NDOVU_KEY_CACHE_TTL_SECONDS`) e o rate limit por chave no servidor.

## Carga de exemplo

```bash
node tools/seed/seed.mjs --sessions 40 \
  --endpoint https://ingest-ndovu-dev.apps.<cluster> \
  --key <chave de dev>
```

É a validação de ponta a ponta do passo 8 da ordem de subida
(`ndovu-gitops/docs/DEPLOY.md`): se as sessões aparecem no explorador, ingestão,
stream, writer, ClickHouse e dashboard estão todos de pé.

## Pipeline

| Etapa | O quê |
|-------|-------|
| Lint/parse | `node --check` em cada SDK |
| Contrato | Valida os payloads gerados contra o `openapi.yaml` publicado pelo `ndovu-backend` |
| Sanity | Importa cada módulo e confere o export `createNdovu` |
| Publish | `npm publish` no registry interno, em tag semver |

**Sem imagem, sem Deployment, sem namespace.** Este repositório não aparece no
`ndovu-gitops`.

## Versionamento

Semver estrito, e o major acompanha a versão do contrato:

- **patch/minor** — recursos novos compatíveis (novo tipo de evento, nova opção)
- **major** — só quando o contrato v1 quebrar

Frontends de terceiros dependem disso. Um SDK que muda o formato do payload
sem bump de major quebra ingestão em produção de aplicações que este
repositório não conhece.

## Contratos com outros repositórios

| Repositório | Relação |
|-------------|---------|
| `ndovu-backend` | Fonte da verdade do contrato (`api/openapi.yaml`). O teste de contrato desta pipeline consome a spec publicada lá |
| Aplicações consumidoras | Instalam via npm; não conhecem o restante da plataforma |

Se o `ndovu-backend` alterar o contrato, a pipeline **daqui** quebra — é
proposital, é o alarme de incompatibilidade.

## Opcional: distribuição por CDN

Se houver demanda por consumo sem npm (script tag), a alternativa é publicar
`ndovu-browser.js` em um bucket S3 com CDN na frente, versionado por caminho
(`/v1.2.0/ndovu-browser.js`). Isso adiciona um passo de publicação à pipeline,
mas continua sem exigir workload no OpenShift.
