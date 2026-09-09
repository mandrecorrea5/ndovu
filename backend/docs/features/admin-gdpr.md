# LGPD / GDPR (`/admin/gdpr`)

## Para que serve

Atende dois direitos essenciais do titular de dados sobre o **end-user
do seu app** (o `userId` que o SDK envia em `session.userId`, não o
usuário do backoffice):

- **Portabilidade** (`export`) — baixa um JSON com todos os eventos
  do usuário, para entrega ou migração.
- **Direito ao esquecimento** (`forget`) — dispara um
  `ALTER TABLE trace_events DELETE` no ClickHouse, removendo os
  eventos do usuário. Ambas as ações caem no
  [audit log](admin-audit-log.md) para retenção legal do próprio
  registro de exclusão.

## Onde fica

- Rota: `/admin/gdpr`
- Arquivo: `dashboard/src/app/admin/gdpr/page.tsx`
- Endpoints:
  - `GET /v1/admin/gdpr/user/{userId}/export`
  - `DELETE /v1/admin/gdpr/user/{userId}`
- Papel mínimo: **admin**

## Como usar

1. Abra **LGPD / GDPR**. Um único campo: **userId (do end-user)**.
2. Cole o `userId` (o mesmo que o SDK envia — pode ser `user-42`,
   um UUID ou qualquer string estável do seu sistema).
3. Clique **Exportar dados (portabilidade)** — o dashboard baixa
   `ndovu-export-<userId>.json` no browser. Aparece a mensagem
   "Exportado — N evento(s) baixados." Limite: 10k eventos por
   export.
4. Para apagar: clique **Esquecer usuário (delete)**. Um `confirm()`
   pede confirmação — "Ação irreversível — recomendado exportar
   antes."
5. Após aceitar, aparece: "Solicitação registrada. ClickHouse aplica
   async — pode levar alguns minutos até desaparecer das consultas."

## O que você vê

- Cabeçalho:  
  "Portabilidade e direito ao esquecimento para **usuários finais**
  do seu app (identificados pelo `userId` que o SDK envia). Todas as
  ações são registradas no audit log."
- Card com o input `userId` e dois botões: **Exportar dados
  (portabilidade)** (primário) e **Esquecer usuário (delete)**
  (vermelho).
- Card informativo "Sobre a implementação":
  - Export: JSON de até 10k eventos, download local.
  - Forget: `ALTER TABLE trace_events DELETE` — mutação assíncrona
    do ClickHouse; a merge de partição aplica em background.
  - Audit: cada ação grava entrada com actor, IP e user-agent.

## Como demonstrar

> "DPO recebeu solicitação por e-mail: 'quero todos os dados do
> user-42' ou 'apague meus dados'. Cola o ID, clica exportar — sai um
> JSON pronto para entrega. Ou clica esquecer — o ClickHouse aplica
> em minutos. O audit log guarda quem executou. Compliance completo
> em três cliques."

Roteiro de 60s:
1. Cole `user-demo-42` no input.
2. Clique **Exportar** — mostre o arquivo `ndovu-export-user-demo-42.json`
   baixado com N eventos.
3. Abra o JSON, mostre estrutura: `userId`, `eventCount`,
   `exportedAt`, `events[]`.
4. Clique **Esquecer** — aceite o confirm, mostre a mensagem sobre
   async.
5. Vá em `/admin/audit-log`, filtre por `gdpr` — mostre as duas
   entradas com IP e user-agent do admin que executou.

## Papéis (RBAC)

- **admin only**. Editor/viewer não veem o link.
- Admin só exporta/apaga usuários **dos apps do próprio tenant** —
  `tenantScope` filtra a busca no ClickHouse pelo lookup do userId
  em eventos scope-válidos.
- `is_super` age cross-tenant — cuidado, é o único papel que apaga
  dados de outras empresas.
- **Não confundir com usuário do backoffice** — o `userId` aqui é
  o end-user do seu produto, não o operador do Ndovu.

## Dependências

- ClickHouse com permissão de `ALTER TABLE DELETE` na tabela
  `trace_events` — configurado no setup do backend.
- Eventos precisam ter `session.userId` preenchido pelo SDK; se seu
  frontend não identifica o usuário, não há como localizar.
- Sessions/replays e feedbacks associados **não** são apagados por
  essa rota — hoje só `trace_events`. Considere excluir manualmente
  em `/admin/feedbacks` se aplicável.

## Perguntas frequentes

- **"Por que o `forget` demora minutos?"**  
  ClickHouse faz `ALTER ... DELETE` como **mutação assíncrona** —
  novas queries eventualmente param de ver os eventos, mas a merge
  de partição em background é o que remove fisicamente.
- **"Consigo desfazer um `forget`?"**  
  Não. Por isso o dashboard sempre recomenda exportar antes e o
  confirm é explícito.
- **"O export inclui replays?"**  
  Não — só `trace_events` da tabela padrão. Snapshots ficam em
  storage separado e não fazem parte do JSON.
- **"E se o usuário tem mais de 10k eventos?"**  
  O export corta em 10k para não explodir o browser. Para volumes
  maiores, use a API diretamente com paginação ou faça a exportação
  em batch pelo backend.

## Referências

- Página: `dashboard/src/app/admin/gdpr/page.tsx`
- API client: `dashboard/src/lib/api.ts` → `gdprExport`, `gdprForget`
- Handler backend: `backend/internal/adapter/httpapi/handlers.go`
  (rotas `gdpr/user/{userId}`)
- Docs relacionadas: [admin-audit-log.md](admin-audit-log.md),
  [admin-feedbacks.md](admin-feedbacks.md)
