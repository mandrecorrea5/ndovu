# Ndovu — Documentação Executiva

> **Público-alvo:** liderança de tecnologia, produto, financeiro e diretoria.
> **Objetivo:** explicar o que o ndovu é, o valor que gera para o negócio, o
> custo, o risco e a maturidade atual em linguagem não-técnica.
>
> Data desta versão: agosto/2026.
> Maturidade: Fase 4 concluída (roadmap completo de 12+ meses entregue).

---

## Sumário

1. [O que é o ndovu](#1-o-que-é-o-ndovu)
2. [Problema que resolve](#2-problema-que-resolve)
3. [Proposta de valor](#3-proposta-de-valor)
4. [Como ele se compara com o mercado](#4-como-ele-se-compara-com-o-mercado)
5. [Impacto financeiro esperado](#5-impacto-financeiro-esperado)
6. [Casos de uso concretos](#6-casos-de-uso-concretos)
7. [Estado de maturidade](#7-estado-de-maturidade)
8. [Custo total de propriedade (TCO)](#8-custo-total-de-propriedade-tco)
9. [Riscos e mitigações](#9-riscos-e-mitigações)
10. [Governança, LGPD e segurança](#10-governança-lgpd-e-segurança)
11. [Roadmap adiante](#11-roadmap-adiante)
12. [Recomendações e próximos passos](#12-recomendações-e-próximos-passos)

---

## 1. O que é o ndovu

O ndovu é uma **plataforma proprietária de observabilidade de frontend** —
uma ferramenta que instalamos nos nossos sites e aplicativos (web, mobile) e
que passa a capturar automaticamente **tudo o que acontece com o usuário**:
telas visitadas, cliques importantes, chamadas de API, erros, lentidões e o
DOM da tela no momento do problema.

Esses dados são organizados por **sessão** (a "história" completa daquele
usuário naquela visita), armazenados em banco analítico próprio e exibidos
em um **dashboard operacional** que substitui, com sobra, a necessidade de
contratar serviços SaaS como Sentry, LogRocket ou Datadog RUM.

Em uma frase:

> **Ndovu = "câmera de segurança do produto digital"** — grava o que o
> usuário fez, o que quebrou, quando quebrou e quem foi impactado. Tudo
> hospedado no nosso ambiente, com custo previsível e código auditável.

---

## 2. Problema que resolve

Toda operação digital sofre de três dores crônicas:

### 2.1. **"Não sabemos que quebrou até o cliente reclamar"**

Erros de JavaScript, quedas de latência, botões que não funcionam em um
navegador específico — hoje descobrimos por ticket de suporte, e só depois
de horas ou dias. Cada minuto sem visibilidade é MTTR (Mean Time To Repair)
que cai direto no NPS e na fatura de suporte.

### 2.2. **"O log existe, mas ninguém acha o que aconteceu"**

Quando o cliente reclama, o time gasta 30-90 minutos vasculhando logs de
backend tentando reconstituir o que o usuário viu. Sem trilha do frontend,
o backend só mostra "chegou uma request" — mas não *o que o usuário estava
tentando fazer* quando aquilo aconteceu.

### 2.3. **"Pagamos SaaS caro, com nossos dados fora"**

Ferramentas como Datadog, Sentry Business e LogRocket cobram por volume de
eventos ou por gigabyte. Um app com 100k usuários/mês facilmente gera fatura
de **US$ 1.000-5.000/mês**, e todos os dados — inclusive PII sensível — ficam
em servidores de terceiros no exterior, com implicações de LGPD.

O ndovu ataca as três dores ao mesmo tempo: **visibilidade** (dashboard,
alertas, digest), **rastreabilidade** (sessão completa + replay do DOM) e
**soberania de dados** (self-hosted em infra própria).

---

## 3. Proposta de valor

### 3.1. Para produto e UX

- **Enxergam o produto pelos olhos do usuário** — funis de conversão,
  cohorts de retenção D1/D7/D14/D30 e session replay mostram *onde* o
  usuário desiste ou trava.
- **Priorizam roadmap com dados reais** — o Impact Score ranqueia
  automaticamente os erros que afetam mais usuários, e não apenas os que
  o time percebeu.

### 3.2. Para desenvolvimento e SRE

- **MTTR ~30-60% menor**: erros aparecem no dashboard em segundos, com
  stack trace desminificado (source maps), breadcrumbs (últimos 30 eventos
  antes do erro) e o **snapshot do DOM** no momento exato.
- **Alertas proativos** (Slack, webhook, Teams) por regra de threshold e
  por **detecção de anomalia** — o próprio ndovu aprende o comportamento
  normal e avisa quando desvia.
- **Correlação frontend ↔ backend** via OpenTelemetry W3C traceparent —
  seguir uma requisição do clique do usuário até a query SQL no backend.

### 3.3. Para liderança e financeiro

- **Custo previsível e fixo** — infra self-hosted (Docker/K8s), sem
  cobrança por evento. Escala verticalmente com storage barato.
- **Dados sob nosso controle** — LGPD-friendly, DPA interno, direito ao
  esquecimento implementado no produto (endpoint próprio).
- **Sem lock-in de fornecedor** — o contrato é aberto e documentado; se
  amanhã quisermos migrar para Sentry ou Datadog, os dados são exportáveis.

### 3.4. Para compliance e jurídico

- **Auditoria imutável** de ações administrativas (quem revogou qual
  chave, quem exportou dados, quem apagou usuário).
- **Direito ao esquecimento (LGPD Art. 18)** via endpoint que apaga
  todos os eventos de um `user_id` no ClickHouse.
- **Exportação de dados de um titular** para atender solicitações.
- **PII redaction** já embarcada no SDK (senhas, tokens, CPF, cartão).

---

## 4. Como ele se compara com o mercado

| Recurso | Sentry Business | Datadog RUM | LogRocket | **ndovu** |
|---|---|---|---|---|
| Captura de erros JS | ✅ | ✅ | ✅ | ✅ |
| Agrupamento por fingerprint | ✅ | ✅ | ✅ | ✅ |
| Source maps | ✅ | ✅ | ✅ | ✅ |
| Web Vitals (LCP, CLS, INP) | ⚠️ | ✅ | ✅ | ✅ |
| Session replay (DOM) | ❌ | ⚠️ | ✅ | ✅ (MVP) |
| Breadcrumbs (últimos eventos antes do erro) | ✅ | ✅ | ✅ | ✅ |
| Funis de conversão | ❌ | ⚠️ | ✅ | ✅ |
| Cohorts de retenção | ❌ | ❌ | ✅ | ✅ |
| Alertas (Slack/webhook) | ✅ | ✅ | ✅ | ✅ |
| Detecção de anomalia | ⚠️ add-on | ✅ | ❌ | ✅ (z-score sazonal) |
| Correlação com backend (OpenTelemetry) | ⚠️ | ✅ | ❌ | ✅ (W3C traceparent) |
| Widget de feedback do usuário | ✅ | ❌ | ❌ | ✅ |
| Retenção em tiers (hot/warm/cold) | ❌ | ⚠️ | ❌ | ✅ (SSD → HDD → S3) |
| Sampling adaptativo server-side | ⚠️ | ✅ | ❌ | ✅ |
| Multi-tenant (multi-cliente) | ✅ | ✅ | ✅ | ✅ |
| RBAC granular por app | ⚠️ | ✅ | ⚠️ | ✅ |
| Digest semanal por email | ✅ | ✅ | ❌ | ✅ |
| Audit log de admin | ✅ | ✅ | ⚠️ | ✅ |
| LGPD: direito ao esquecimento | ⚠️ | ⚠️ | ⚠️ | ✅ (endpoint próprio) |
| **Self-hosted (dados nossos)** | ❌ | ❌ | ❌ | ✅ |
| **Custo por 1M eventos/mês** | US$ 26+ | US$ 150+ | US$ 200+ | ~US$ 15 (infra) |
| **Custo por 100M eventos/mês** | US$ 2.600+ | US$ 15k+ | US$ 20k+ | ~US$ 80-150 (infra) |

**Interpretação:** o ndovu cobre 95% dos recursos das ferramentas líderes,
com **paridade funcional nos itens críticos** (erros, replay, funis,
alertas). Os itens marcados ⚠️ nos concorrentes exigem add-ons pagos ou
planos superiores. O único item onde o ndovu ainda está atrás é
*maturidade de session replay* — hoje é MVP (snapshot no momento do
erro), enquanto LogRocket faz replay contínuo tipo vídeo. Está no
roadmap evoluir para rrweb.

---

## 5. Impacto financeiro esperado

### 5.1. Redução de custo direto (SaaS)

Cenário conservador (empresa com ~10 apps, 5 milhões de eventos/mês):

| Item | Sentry + LogRocket | ndovu |
|---|---|---|
| Assinatura de ferramenta | ~US$ 1.500/mês | US$ 0 |
| Infraestrutura (VPS + storage) | US$ 0 | ~US$ 100/mês |
| Egress / bandwidth | ~US$ 50/mês | ~US$ 20/mês |
| **Total mensal** | **~US$ 1.550** | **~US$ 120** |
| **Total anual** | **~US$ 18.600** | **~US$ 1.440** |
| **Economia anual** | — | **~US$ 17.000 (~R$ 85 mil)** |

Cenário empresa maior (100M eventos/mês): economia de **US$ 200k+ ao ano**.

### 5.2. Redução de MTTR

- Baseline típico de MTTR sem observabilidade: 2-4h por incidente.
- Baseline com ndovu instrumentado: **20-40 minutos** (o time recebe alerta
  no Slack com stack trace desminificado + snapshot do DOM + breadcrumbs).
- Cada hora de MTTR reduzida representa:
  - Menos tickets de suporte gerados
  - Menos churn de usuário
  - Menos custo de plantão / bombeiro

Estimativa conservadora para empresa média (20 incidentes/mês, ganho de 1h
por incidente, custo hora-time de US$ 100): **US$ 24.000/ano em produtividade
recuperada**.

### 5.3. Aumento de conversão

Detecção rápida de anomalia + funis previnem quedas silenciosas em rotas
críticas (checkout, login, cadastro). Recuperar 1% de conversão em uma
operação de US$ 10M/ano = **US$ 100k**. Um único bug de checkout escondido
por 72h paga o ndovu por vários anos.

---

## 6. Casos de uso concretos

### 6.1. "Caiu o pagamento no iOS 17"

O time de produto recebe um ticket. Antes do ndovu, isso viraria uma
investigação de 4-6 horas envolvendo backend, front, QA e suporte.

Com o ndovu:

1. Abre `/issues`, filtra por `feature=pagamento` e `user_agent contém iOS 17`.
2. Vê 340 ocorrências agrupadas por fingerprint, com 87 usuários afetados.
3. Clica na issue, vê o stack trace desminificado apontando para a linha
   exata do bug e o snapshot do DOM mostrando que o botão de pagamento
   estava sobreposto por um modal.
4. Cria um comentário atribuindo ao Fulano.
5. Após deploy, marca a issue como `resolved`.
6. Se voltar a ocorrer após o próximo release, o sistema detecta a
   regressão e reabre a issue automaticamente.

**Tempo total: 15 minutos, não 4 horas.**

### 6.2. "Por que a taxa de conversão caiu?"

O time de produto abre `/funnels`, executa o funil de onboarding
(cadastro → confirmação → primeira compra) filtrado por semana:

- Semana passada: 8.400 iniciaram, 3.200 concluíram (38%).
- Semana atual: 8.100 iniciaram, 2.100 concluíram (26%).

Vê que a queda concentra no passo 3 (primeira compra). Filtra `/traces`
por `screen=/checkout` e `type=http_request` na semana atual, encontra um
pico de HTTP 503 na rota `/api/gateway-pagamento`. Junta com o dashboard
de anomalias e confirma: o gateway começou a falhar terça-feira.

**Sem ndovu: 3 dias e uma reunião de guerra. Com ndovu: 20 minutos.**

### 6.3. "Auditoria de LGPD"

O DPO recebe uma solicitação de titular pedindo:

1. Exportação de todos os dados capturados sobre ele.
2. Exclusão total.

O admin executa:

- `POST /v1/admin/gdpr/user/{userId}/export` → devolve JSON com todos os
  eventos capturados.
- `DELETE /v1/admin/gdpr/user/{userId}` → apaga do ClickHouse.

Todas as ações ficam no `audit_log` insert-only. Comprovação para a ANPD
em minutos, com trilha imutável.

### 6.4. "Digest semanal para o CEO"

Toda segunda 08h, o CEO recebe email automático:

- Top 10 erros da semana (com Impact Score).
- Web Vitals médios por app (p75 de LCP, INP).
- Anomalias detectadas.
- Feedbacks negativos abertos.

Sem que ninguém precise gerar relatório manual.

---

## 7. Estado de maturidade

O produto passou por 4 fases de amadurecimento, cada uma com KPIs próprios.
**Todas concluídas.**

### Fase 1 — Fundação (concluída)
Handlers globais no SDK, Web Vitals, breadcrumbs, agrupamento de erros
por fingerprint, alertas via Slack/webhook, rate limit, CI, métricas
Prometheus, SDK Node.js.

### Fase 2 — Diferenciação e Insights (concluída)
Source maps + resolução de stack, release tracking, saved views, funis
de conversão, retenção D1/D7/D14/D30, triagem de issues (assignee,
comentários), digest semanal por email, SDK React Native, SDK Next.js,
OpenTelemetry W3C traceparent.

### Fase 3 — Enterprise-ready (concluída)
Multi-tenancy (companies + apps), RBAC granular por app, audit log
imutável, LGPD (export + esquecimento), retenção em camadas
(hot/warm/cold com S3), sampling adaptativo server-side, session
replay MVP (snapshot do DOM em erro).

### Fase 4 — Diferenciais Avançados (concluída)
Detecção de anomalia com z-score sazonal, factory de mailer (SendGrid
+ SMTP com auto-detecção), User Feedback Widget (bug/sugestão/elogio
via SDK).

**Total de código:** ~25.000 linhas (Go 1.25 + TypeScript/React + SDKs).
**Cobertura de testes:** testes unitários em pontos críticos (fingerprint,
ingest, auth, anomaly, sampling, stream, handlers).

---

## 8. Custo total de propriedade (TCO)

### 8.1. Infra mínima para produção (10 apps, 5M eventos/mês)

| Componente | Especificação | Custo mensal |
|---|---|---|
| VPS API + Writer | 2 vCPU, 4 GB RAM | ~US$ 20 |
| VPS Postgres | 1 vCPU, 2 GB RAM | ~US$ 12 |
| VPS ClickHouse | 2 vCPU, 8 GB RAM, 100 GB SSD | ~US$ 40 |
| VPS Dashboard + NATS | 1 vCPU, 2 GB RAM | ~US$ 12 |
| S3 (cold + snapshots) | ~50 GB | ~US$ 5 |
| Bandwidth / egress | ~200 GB | ~US$ 15 |
| Backup (snapshot diário) | ~30 GB | ~US$ 3 |
| **Total** | — | **~US$ 107/mês** |

### 8.2. Custos ocultos

- **Manutenção:** ~4h/mês de um engenheiro para monitorar, ajustar TTL,
  aplicar updates de dependências. Custo: ~US$ 400/mês.
- **Onboarding de novo app:** ~2h iniciais (criar app, gerar chave,
  instalar SDK). Depois, ~0 custo.
- **Treinamento do time:** 1-2h de walkthrough do dashboard. Documentação
  interna completa (4 documentos, incluindo este).

### 8.3. TCO consolidado (12 meses)

| Item | 12 meses |
|---|---|
| Infra | US$ 1.284 |
| Manutenção (0.5 FTE dedicado) | US$ 4.800 |
| Onboarding inicial | US$ 500 (uma vez) |
| **TCO total** | **~US$ 6.500 no primeiro ano** |
| **TCO SaaS equivalente** | **~US$ 18.600-20.000** |
| **Economia** | **US$ 12.000-14.000/ano** |

Payback do investimento de setup: **< 1 mês**.

---

## 9. Riscos e mitigações

### 9.1. Risco de indisponibilidade

**Risco:** o ndovu cair pode nos deixar cegos.
**Mitigação:**
- A ingestão retorna `202 Accepted` em ~1ms e enfileira no NATS
  JetStream — mesmo se o ClickHouse cair, os eventos ficam retidos
  por 48h e são reprocessados quando volta.
- A API é stateless — pode escalar horizontalmente.
- O writer é resiliente: reinicia consumindo do último offset ackado.
- Endpoints `/health` e `/metrics` (Prometheus) permitem monitoramento
  externo (uptime robot, PagerDuty).

### 9.2. Risco de custo explodir

**Risco:** volume de eventos disparar (bug em SDK, ataque).
**Mitigação:**
- Rate limit por API key (50 req/s default, configurável).
- Sampling adaptativo server-side (descarta % de eventos de menor valor,
  mantém 100% dos erros).
- TTL agressivo por tier (dados frios movem automaticamente para S3, e
  são apagados após 90 dias).

### 9.3. Risco de vulnerabilidade de segurança

**Risco:** endpoint público exposto, credenciais vazadas.
**Mitigação:**
- Chaves de ingestão são hasheadas (bcrypt) no banco, exibidas 1x na
  criação, revogáveis em 30s (TTL do cache).
- JWT com segredo rotacionável (HS256).
- Middleware `maxBody` (1 MB default) previne payloads gigantes.
- CORS configurável.
- Audit log imutável de todas as ações administrativas.
- Nenhuma dependência de vendor externo em runtime (sem risco de supply
  chain de SaaS).

### 9.4. Risco de perda de dados

**Risco:** falha do storage, ClickHouse corrompido.
**Mitigação:**
- Cold tier em S3 (dados > 30 dias já ficam em objeto durável).
- Backup diário do Postgres (control plane).
- Snapshot semanal do ClickHouse (dumps para S3).
- NATS JetStream é durável (não perde eventos se o writer cair).

### 9.5. Risco de descontinuidade

**Risco:** e se quem mantém o produto internamente sair?
**Mitigação:**
- Código-fonte 100% nosso, com documentação (4 docs completas).
- Stack composta de tecnologias populares (Go, ClickHouse, Postgres,
  NATS, Next.js) — qualquer engenheiro sênior assume.
- Contrato de ingestão aberto e documentado — se decidirmos migrar para
  SaaS, o SDK continua funcionando enquanto a fatura roda em paralelo.

---

## 10. Governança, LGPD e segurança

### 10.1. Aderência à LGPD

| Artigo LGPD | Requisito | Implementação |
|---|---|---|
| Art. 6º (finalidade) | Uso legítimo do dado | Dados de sessão anônima; usuário identificado só se o app enviar `userId` explicitamente |
| Art. 9º (informação) | Transparência | DPA público, política de retenção documentada (90 dias) |
| Art. 15º (término) | Descarte após finalidade | TTL automático apaga eventos após 90 dias |
| Art. 18º (direitos do titular) | Acesso e eliminação | Endpoints `/gdpr/user/{id}/export` e `DELETE /gdpr/user/{id}` |
| Art. 46º (segurança) | Medidas técnicas | Bcrypt, TLS, RBAC, audit log, rate limit |
| Art. 48º (comunicação de incidentes) | Notificação | Alertas configuráveis, digest semanal |

### 10.2. Segurança em profundidade

- **Autenticação:** JWT HS256 com TTL de 8h (configurável).
- **Autorização:** roles (admin/viewer) + RBAC granular (viewer pode
  ter acesso a apps específicos).
- **Chaves de ingestão:** bcrypt no banco, exibidas 1x, revogáveis
  instantaneamente.
- **Redação de PII:** o SDK remove `password`, `token`, `cvv`, `card`,
  `cartão`, `senha` **antes** de sair do browser. Nada sensível chega
  ao servidor.
- **Snapshots do DOM:** mascaram automaticamente `input[type=password]`,
  `input[type=email]`, campos com `data-ndovu-mask`.
- **Multi-tenancy:** query enforcement no middleware `tenantScope` —
  viewer nunca vê dado de outra empresa por design.
- **Cross-tenant guard:** ingestão valida que `envelope.app` bate com a
  API key usada; tentar enviar como outro app retorna 400.

### 10.3. Auditoria

Todas as ações admin (login, criar/apagar chave, revogar acesso,
exportar dados de titular, apagar usuário) ficam gravadas em
`audit_log` insert-only, com:
- Quem (`actor_user_id`, `actor_email`).
- O quê (`action`, `resource_type`, `resource_id`).
- Quando (`created_at`).
- Onde (`ip`, `user_agent`).
- Detalhes (JSON livre).

---

## 11. Roadmap adiante

A Fase 4 fechou o roadmap original. As próximas evoluções possíveis, em
ordem de valor esperado, são:

### 11.1. Curto prazo (0-3 meses)

- **Session replay contínuo (rrweb)** — substitui o snapshot único por
  gravação contínua tipo vídeo. Fecha o gap com LogRocket.
- **Feature flags simples** — kill switch integrado a eventos, para
  reverter mudanças rapidamente sem redeploy.
- **Integração com Jira/Linear/GitHub** — botão "criar ticket a partir
  da issue" com contexto (stack + replay + breadcrumbs) já preenchido.
- **Helm chart para Kubernetes** — hoje o deploy é Docker Compose.

### 11.2. Médio prazo (3-6 meses)

- **Query API pública (SQL-as-a-service)** — expõe views seguras para
  BI (Metabase, Redash, Superset) sem acesso direto ao ClickHouse.
- **SSO / OIDC** — Keycloak, Google Workspace, Microsoft Entra.
  Requisito para adoção corporativa.
- **SLOs e error budgets** — definir metas (ex.: 99.5% de sessões sem
  erro), medir queima, alertar em burn rate acelerado.
- **Export para data warehouse** — streaming para BigQuery/Snowflake.

### 11.3. Longo prazo (6-12 meses)

- **AI-assisted triage** — usar LLM para agrupar issues semelhantes,
  sugerir causa provável a partir do stack + breadcrumbs, redigir
  primeira resposta ao ticket.
- **Synthetic monitoring** — bot que executa jornadas críticas
  periodicamente para detectar quebras antes do usuário.
- **Marketplace de dashboards** — presets prontos para verticais
  específicos (ecommerce, financeiro, saúde).

---

## 12. Recomendações e próximos passos

### 12.1. Se você é liderança de produto ou tecnologia

**Recomendação:** instrumentar 100% dos apps de produção com o SDK do
ndovu nos próximos 60 dias. Priorizar apps de maior tráfego e apps
críticos (checkout, login, cadastro).

**Próximos passos concretos:**
1. Escolher 1 app-piloto (idealmente o de maior volume de tickets de
   suporte).
2. Fazer instrumentação em 1 sprint (~1 semana com SDK plug-and-play).
3. Rodar em paralelo com ferramenta atual (Sentry/LogRocket/nenhuma) por
   30 dias.
4. Comparar métricas: MTTR médio, alertas capturados, tickets de suporte
   evitados.
5. Se o KPI melhorar em ≥30%, expandir para os demais apps.

### 12.2. Se você é liderança financeira

**Recomendação:** validar TCO com o time de tecnologia e considerar
substituir SaaS existentes na próxima renovação. O payback é sub-mês.

**Próximos passos:**
1. Levantar contratos ativos de Sentry, LogRocket, Datadog RUM ou
   equivalentes.
2. Comparar com TCO deste documento (Seção 8).
3. Alinhar cronograma de descomissionamento com fim do ciclo de
   renovação.

### 12.3. Se você é DPO ou compliance

**Recomendação:** documentar o ndovu como controlador interno de dados
de observabilidade. Atualizar RIPD (Relatório de Impacto à Proteção de
Dados) e política de privacidade.

**Próximos passos:**
1. Ler a Seção 10 (Governança).
2. Fazer inspeção do audit log e dos endpoints de LGPD (existem, estão
   auditados).
3. Ajustar cláusula na política de privacidade externa mencionando
   "coletamos dados anônimos de uso para melhoria contínua, com
   retenção de 90 dias, sob nosso controle direto".

### 12.4. Se você é diretoria

**Recomendação:** posicionar o ndovu como **capacidade estratégica
interna**, não como projeto pontual. É um ativo que:

- Reduz custo direto (SaaS).
- Reduz custo indireto (MTTR, tickets, churn).
- Aumenta receita indiretamente (conversão + retenção mensuráveis).
- Blinda a empresa contra LGPD.
- Dá autonomia de dados (sem terceiro nos olhando).

Vale considerar a criação de um **time squad dedicado** (2-3 pessoas)
para evoluir o roadmap adiante, atendendo demandas de outras áreas
(dados, produto, segurança) que passam a consumir os insights.

---

## Apêndices

### A.1. Glossário rápido

- **MTTR** — *Mean Time To Repair*: tempo médio entre detectar o
  problema e resolvê-lo.
- **RUM** — *Real User Monitoring*: monitoramento do usuário real
  (não simulado).
- **Fingerprint** — assinatura única de um erro (tipo + mensagem +
  stack normalizado) usada para agrupar ocorrências.
- **Impact Score** — score composto (usuários afetados × frequência ×
  recência) que ranqueia issues.
- **Session replay** — reprodução visual do que o usuário viu (do
  snapshot do DOM ao vídeo).
- **Session** — coleção de todos os eventos de um usuário durante uma
  visita (login → navegação → erro → saída).
- **Breadcrumb** — evento de baixa criticidade que precede um erro
  (clique, navegação, chamada HTTP) e ajuda no diagnóstico.
- **Fingerprint** — hash usado para agrupar erros idênticos.
- **Sampling** — descarte controlado de % de eventos para reduzir volume
  mantendo os importantes (erros).

### A.2. Documentação complementar

- **[TECNICA.md](TECNICA.md)** — para desenvolvedores que vão integrar o
  SDK e trabalhar com a API.
- **[FUNCIONAL.md](FUNCIONAL.md)** — para product owners, QAs e
  designers que vão usar o dashboard.
- **[ARQUITETURA.md](ARQUITETURA.md)** — para arquitetos e tech leads
  que precisam entender decisões de design e trade-offs.
- **[CONTRACT.md](CONTRACT.md)** — spec do contrato de ingestão v1.
- **[INTEGRATION.md](INTEGRATION.md)** — guia passo-a-passo de
  integração.

### A.3. Contato e suporte

O produto é mantido internamente. Para dúvidas, priorização ou
solicitações de novas funcionalidades, procure o time responsável (ver
CODEOWNERS do repositório).

---

**Assinatura desta versão.**
Documento vivo. Última atualização: agosto/2026.
Fase 4 (Diferenciais Avançados) concluída. Roadmap adiante em discussão.
