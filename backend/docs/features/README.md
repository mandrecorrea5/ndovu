# Ndovu — Guia por funcionalidade

Um documento por funcionalidade, organizado para você conseguir **usar,
demonstrar e explicar** cada parte da plataforma. Cada guia responde:

- **Para que serve** — o problema que resolve.
- **Como usar** — passo a passo prático.
- **Papéis (RBAC)** — quem enxerga o quê.
- **Como demonstrar** — o pitch quando você mostra para alguém.
- **Perguntas frequentes** — o que costuma travar a conversa.
- **Referências** — código, endpoints e docs relacionados.

> **Onde tudo mora:** dashboard em `http://localhost:13000`, API em
> `http://localhost:18081`, admin bootstrap `admin@ndovu.local` /
> `admin12345` (dev).

---

## Consulta e análise

| Guia | Rota | Público |
|---|---|---|
| [Visão geral (overview)](visao-geral.md) | `/` | qualquer papel |
| [Explorador de traces](traces.md) | `/traces` | qualquer papel |
| [Sessões e timeline](sessoes.md) | `/sessions`, `/sessions/[id]` | qualquer papel |
| [Issues (erros agrupados)](issues.md) | `/issues`, `/issues/[fingerprint]` | qualquer papel; editar precisa editor+ |
| [Performance (Web Vitals)](performance.md) | `/performance` | qualquer papel |
| [Releases](releases.md) | `/releases` | qualquer papel |
| [Retenção (cohorts)](retention.md) | `/retention` | qualquer papel |
| [Funis de conversão](funnels.md) | `/funnels` | ver: qualquer; editar: editor+ |
| [Saved views](saved-views.md) | menu `Views` em telas com filtro | criar: qualquer; compartilhar: editor+ |

## Administração

| Guia | Rota | Público |
|---|---|---|
| [Empresas (companies)](admin-companies.md) | `/admin/companies` | admin |
| [Apps](admin-apps.md) | `/admin/apps` | admin |
| [Usuários e permissões](admin-users.md) | `/admin/users` | admin |
| [Chaves de API](admin-keys.md) | `/admin/keys` | admin |
| [Alertas](admin-alerts.md) | `/admin/alerts` | admin |
| [Anomalias](admin-anomalies.md) | `/admin/anomalies` | admin |
| [Sampling adaptativo](admin-sampling.md) | `/admin/sampling` | admin |
| [Source Maps](admin-source-maps.md) | `/admin/source-maps` | admin |
| [Feedback do usuário](admin-feedbacks.md) | `/admin/feedbacks` | admin |
| [Audit Log](admin-audit-log.md) | `/admin/audit-log` | admin |
| [LGPD / GDPR](admin-gdpr.md) | `/admin/gdpr` | admin |

## Plataforma (sem tela dedicada)

| Guia | Superfície | Público |
|---|---|---|
| [Login e sessão (JWT)](autenticacao.md) | `/login`, `/v1/auth/*` | todos |
| [SDK Browser](sdk-browser.md) | `sdk/ndovu-browser.js` | devs integradores |
| [Ingestão de eventos](ingest-eventos.md) | `POST /v1/events` | devs integradores |
| [Snapshots (session replay)](ingest-snapshots.md) | `POST /v1/snapshots` | devs integradores |
| [Widget de feedback](widget-feedback.md) | `POST /v1/feedbacks` | devs integradores |
| [Digest semanal por e-mail](digest-semanal.md) | scheduler no writer | admin / diretoria |

---

## Convenções usadas nos guias

- **admin / editor / viewer** = papéis do RBAC. Um mesmo usuário pode
  ainda ter `is_super` (bypass total). Ver
  [Usuários e permissões](admin-users.md).
- **Tenant scope** = filtro invisível que restringe automaticamente cada
  query ao escopo do usuário (company + N apps). Ver [ARQUITETURA.md §9](../ARQUITETURA.md).
- **Fingerprint** = hash estável que agrupa erros parecidos. Ver
  [Issues](issues.md).

## Para aprofundar

- [`../USO.md`](../USO.md) — rotinas de operação (day-2).
- [`../FUNCIONAL.md`](../FUNCIONAL.md) — panorama funcional.
- [`../ARQUITETURA.md`](../ARQUITETURA.md) — decisões e trade-offs.
- [`../TECNICA.md`](../TECNICA.md) — endpoints e exemplos de código.
- [`../TESTING.md`](../TESTING.md) — plano e status dos testes.
