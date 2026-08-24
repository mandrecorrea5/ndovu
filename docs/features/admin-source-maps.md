# Source maps (`/admin/source-maps`)

## Para que serve

Stack traces de produção chegam **minificadas** — nomes de função
viram `a`, `b`, linha inteira colapsa em uma coluna. Essa tela recebe
os arquivos `.map` do build por **app + release + filename**; o
backend usa esses maps para **desminificar sob demanda** quando você
abre um erro no dashboard. Sem source map, você lê `at t.f (main.abc.js:1:8432)`;
com map, `at handleSubmit (src/Checkout.tsx:118:14)`.

## Onde fica

- Rota: `/admin/source-maps`
- Arquivo: `dashboard/src/app/admin/source-maps/page.tsx`
- Endpoints:
  - `GET /v1/admin/source-maps?app=&release=`
  - `POST /v1/admin/source-maps` (payload com `content` do JSON do map)
  - `DELETE /v1/admin/source-maps/{id}`
  - Consumidor: `GET /v1/events/{id}/resolved-stack` (viewer+)
- Papel mínimo: **admin**

## Como usar

1. Abra **Source maps**. A tabela lista por app, release, filename,
   tamanho e data de upload.
2. Clique **Novo upload** e preencha:
   - **App** — dropdown de apps cadastrados.
   - **Release** (ex.: `v1.2.3` ou o hash do build) — precisa **bater
     exatamente** com o `release` que o SDK envia no evento.
   - **Filename** — nome do JS minificado (ex.: `main.abc.js`). Se
     você usar o input de arquivo, ele preenche automaticamente
     (removendo o sufixo `.map`).
   - **Arquivo .map** — input `<file>` que lê o conteúdo local; ou
     cole o JSON no textarea abaixo (útil pra CI via `curl`).
3. Clique **enviar**. Reenvio do mesmo `(app, release, filename)`
   **sobrescreve** o anterior.
4. Para excluir: **remover** na linha, com confirmação.

## O que você vê

- Cabeçalho com texto:  
  "Suba os `.map` por release para desminificar stack traces na
  visualização de erros. Reenvio do mesmo arquivo sobrescreve."
- Tabela: **App**, **Release** (em cor de destaque), **Arquivo**,
  **Tamanho** (KB), **Enviado em**, **Ações**.
- Estado vazio: "Nenhum source map enviado ainda."
- Modal com dropdown de app, inputs de release/filename, input de
  arquivo e textarea grande em fonte mono para o JSON bruto.

## Como demonstrar

> "Sem source map, um erro de produção é um enigma. Com map, você
> abre o evento e a stack trace já vem em nome de função e caminho de
> arquivo original. O Ndovu resolve sob demanda — o `.map` fica no
> back, nunca é servido ao browser."

Roteiro de 60s:
1. Faça build do frontend: `pnpm build` gera `main.abc.js` e
   `main.abc.js.map`.
2. Vá em **Novo upload**, escolha `portal-cliente`, release
   `v1.2.3`, selecione o `.map` — filename autofilla.
3. Enviar. Abra um evento de erro do release `v1.2.3` no dashboard.
4. Aponte para a stack: "sem map, seria `t.f`; agora aparece
   `handleSubmit (src/Checkout.tsx:118:14)`".
5. Substitua enviando de novo o mesmo release/filename com um `.map`
   diferente — mostre que a tabela reflete a nova data.

## Papéis (RBAC)

- **admin only** para upload/exclusão. Editor/viewer não veem o link.
- **Consulta de stack resolvida** (`/v1/events/{id}/resolved-stack`)
  fica disponível para **viewer+** — todo mundo enxerga o stack
  legível na tela de evento.
- Admin só sobe/lista maps de apps do próprio tenant.

## Dependências

- Precisa de **app cadastrado**. O `release` no upload deve casar
  exatamente com `event.release` que o SDK envia. Se o front esquecer
  de setar `release`, a resolução nunca acha o map.
- O `.map` fica armazenado no backend (banco, não em blob externo por
  default) — dimensione retenção pensando em quantos releases por
  semana × tamanho médio (~200KB a 2MB por map).

## Perguntas frequentes

- **"Como automatizar upload no CI?"**  
  `curl -X POST /v1/admin/source-maps -H "Authorization: Bearer $TOKEN"
  -d '{"app":"...","release":"...","filename":"...","content":"..."}'`
  — o textarea existe para provar que é só um POST JSON.
- **"O `.map` é servido para o browser?"**  
  Não. Fica só no backend; a resolução é server-side. Nada de vazar
  código-fonte por acidente.
- **"Preciso subir um map por release ou por arquivo?"**  
  Por **arquivo** (bundle) por **release**. Se seu build tem 4
  bundles (`main`, `vendor`, `chunk-a`, `chunk-b`), suba 4 uploads
  para cada release.
- **"E se eu subir o `.map` errado?"**  
  Sobrescreva com o correto no mesmo `(app, release, filename)`. Ou
  **remover** e reenviar.

## Referências

- Página: `dashboard/src/app/admin/source-maps/page.tsx`
- API client: `dashboard/src/lib/api.ts` → `listSourceMaps`,
  `uploadSourceMap`, `deleteSourceMap`, `resolvedStack`
- Resolução backend: `backend/internal/usecase/sourcemaps`
- Docs relacionadas: [admin-apps.md](admin-apps.md)
