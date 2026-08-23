# Ndovu — Documentação Funcional

> **Público-alvo:** product owners, product managers, QA, UX designers,
> analistas de negócio e times de suporte.
>
> **Objetivo:** descrever, em linguagem de produto, todas as
> funcionalidades da plataforma — o que cada tela faz, como o usuário
> interage e o que esperar como resultado. Sem detalhes de implementação.
>
> Data desta versão: agosto/2026.
> Cobre 100% do produto após conclusão da Fase 4.

---

## Sumário

1. [Visão geral do produto](#1-visão-geral-do-produto)
2. [Personas e perfis de acesso](#2-personas-e-perfis-de-acesso)
3. [Navegação e estrutura do dashboard](#3-navegação-e-estrutura-do-dashboard)
4. [Módulo: Visão geral (Overview)](#4-módulo-visão-geral-overview)
5. [Módulo: Issues](#5-módulo-issues)
6. [Módulo: Releases](#6-módulo-releases)
7. [Módulo: Performance (Web Vitals)](#7-módulo-performance-web-vitals)
8. [Módulo: Funis](#8-módulo-funis)
9. [Módulo: Retenção](#9-módulo-retenção)
10. [Módulo: Explorador de eventos](#10-módulo-explorador-de-eventos)
11. [Módulo: Sessões](#11-módulo-sessões)
12. [Módulo: Session replay](#12-módulo-session-replay)
13. [Administração: Empresas](#13-administração-empresas)
14. [Administração: Apps](#14-administração-apps)
15. [Administração: Usuários e permissões](#15-administração-usuários-e-permissões)
16. [Administração: Chaves de API](#16-administração-chaves-de-api)
17. [Administração: Alertas](#17-administração-alertas)
18. [Administração: Anomalias](#18-administração-anomalias)
19. [Administração: Sampling](#19-administração-sampling)
20. [Administração: Source Maps](#20-administração-source-maps)
21. [Administração: Feedback do usuário](#21-administração-feedback-do-usuário)
22. [Administração: Audit Log](#22-administração-audit-log)
23. [Administração: LGPD](#23-administração-lgpd)
24. [Funcionalidades do SDK visíveis ao usuário final](#24-funcionalidades-do-sdk-visíveis-ao-usuário-final)
25. [Fluxos ponta-a-ponta](#25-fluxos-ponta-a-ponta)
26. [Notificações e digests](#26-notificações-e-digests)

---

## 1. Visão geral do produto

O ndovu é uma **plataforma de observabilidade de frontend** — uma
ferramenta que grava tudo o que o usuário faz nos nossos sites e apps
(cliques, telas, chamadas de API, erros, quedas de performance) e
apresenta esses dados em um dashboard operacional.

Ele é composto por dois grandes blocos funcionais:

1. **Ingestão automática via SDK** — cada aplicação (site, app mobile,
   API interna) instala um pequeno pacote (SDK) que captura os eventos
   automaticamente e envia para o backend do ndovu.

2. **Dashboard de análise** — uma interface web onde o time acessa os
   dados capturados, investiga incidentes, mede conversão, configura
   alertas e administra permissões.

O produto atende **três grandes casos de uso**:

- **Diagnosticar problemas em produção rapidamente** (issues, session
  replay, breadcrumbs, alertas).
- **Entender o comportamento do usuário** (funis, retenção, jornadas
  completas em sessões, feedback direto do widget).
- **Governar dados com responsabilidade** (multi-tenant, RBAC granular,
  LGPD, audit log).

---

## 2. Personas e perfis de acesso

### 2.1. Administrador (`admin`)

**Quem é:** líder técnico, tech lead, gerente de operações digitais.

**O que pode fazer:**
- Ver todos os dados de todas as apps da sua empresa.
- Cadastrar empresas, apps e chaves de ingestão.
- Cadastrar e gerir outros usuários.
- Configurar alertas, regras de anomalia, regras de sampling.
- Fazer upload de source maps.
- Executar operações de LGPD (exportar/apagar dados de usuário).
- Consultar o audit log.
- Gerir triagem de feedbacks.

**Restrição:** o sistema garante que sempre exista pelo menos 1
administrador ativo — não é possível se auto-desativar como último admin.

### 2.2. Super-administrador (`is_super`)

**Quem é:** operador da plataforma (dono do produto), enxerga todas as
empresas simultaneamente.

**O que pode fazer:** tudo que o admin faz, mas sem filtro por empresa
(vê todas as companies). Usado tipicamente para operação centralizada.

### 2.3. Viewer / Observador (`viewer`)

**Quem é:** desenvolvedor, product manager, QA, analista de suporte.

**O que pode fazer:**
- Ler todos os dados (eventos, sessões, issues, funis, retenção).
- Salvar visualizações filtradas (saved views).
- Comentar em issues, atribuir dono, alterar status.
- Ver replay de sessão.

**O que NÃO pode fazer:**
- Alterar configurações administrativas.
- Cadastrar usuários, empresas, apps ou chaves.
- Fazer upload de source map.
- Executar operações de LGPD.

### 2.4. Viewer com escopo de app (RBAC granular)

Um viewer pode receber permissão explícita para apps específicos. Nesse
caso, ele **só enxerga dados das apps concedidas** — o restante do
dashboard funciona igual, mas todos os filtros e listagens são
automaticamente limitados ao conjunto autorizado.

Exemplo: um analista do time "Cobrança" recebe acesso apenas às apps
`portal-cobranca` e `app-cobranca-mobile`. Ele nunca verá dados do
`portal-vendas`.

---

## 3. Navegação e estrutura do dashboard

O dashboard é dividido em duas seções principais no menu lateral:

### 3.1. Seção principal (todos os usuários autenticados)

- **Visão geral** — home com KPIs macro
- **Issues** — erros agrupados
- **Releases** — comparativo por versão
- **Performance** — Web Vitals
- **Funis** — conversão passo-a-passo
- **Retenção** — cohorts
- **Explorador** — filtro livre de eventos
- **Sessões** — jornadas completas

### 3.2. Seção administrativa (só para admin)

- **Empresas**
- **Apps**
- **Usuários**
- **Chaves de API**
- **Alertas**
- **Source maps**
- **Audit log**
- **LGPD**
- **Sampling**
- **Anomalias**
- **Feedback**

### 3.3. Elementos comuns

- **Cabeçalho:** nome do usuário logado, seletor de empresa (para
  super-admin), botão sair.
- **Sidebar:** colapsável (guarda preferência no localStorage).
- **Filtros por URL:** todas as telas com filtros mantêm o estado na
  querystring — links são compartilháveis via chat/ticket.
- **Presets (Saved Views):** o usuário salva combinações de filtros por
  tela ("issues abertas do meu time", "sessões com erro no checkout") e
  reabre com 1 clique. Pode compartilhar com o time.

---

## 4. Módulo: Visão geral (Overview)

**Rota:** `/`

**O que mostra:**

- **Cartões de KPI** no topo:
  - Total de eventos no período selecionado
  - Total de sessões únicas
  - Total de usuários únicos
  - Taxa de erro (% de eventos do tipo `error`)
- **Gráfico de série temporal** com 3 linhas:
  - Eventos por hora (barra)
  - Erros por hora (linha)
  - Sessões por hora (linha)
- **Top rotas por p95 de latência** (tabela).
- **Top erros por contagem** (tabela).
- **Distribuição por app** (donut).

**Filtros disponíveis:**
- Período (últimas 24h, 7d, 30d ou intervalo customizado).
- App emissor.
- Feature.

**Casos de uso:**
- Analista abre pela manhã para ver "está tudo bem?".
- Líder olha antes da reunião de status para trazer números.
- Investigação inicial de incidente: "quando começou o pico?".

---

## 5. Módulo: Issues

**Rota:** `/issues` (lista) e `/issues/{fingerprint}` (detalhe).

### 5.1. Lista de issues

Um "issue" é um **grupo de ocorrências do mesmo erro** — ao invés de
mostrar 10.000 eventos idênticos, o sistema agrupa por fingerprint
(hash de tipo + mensagem + stack normalizado) e mostra 1 linha.

**Colunas da lista:**
- Nome / mensagem do erro
- Código do erro (ex.: `AUTH_401`, `NETWORK_ERROR`, `JS_ERROR`)
- App emissor
- Contagem (quantas ocorrências no período)
- Usuários afetados (distinct)
- Primeira vez visto
- Última vez visto
- Impact Score (ranking automático)
- Status (aberto, investigando, resolvido, ignorado)
- Responsável (assignee)

**Filtros:**
- Período, app, feature, status, assignee.
- Busca por mensagem.

**Ordenação padrão:** por Impact Score decrescente.

### 5.2. Detalhe da issue

Ao clicar em uma issue:

- **Cabeçalho** com stack trace desminificado (se houver source map
  correspondente), primeira e última ocorrência, contagem total.
- **Sparkline** de ocorrências ao longo do tempo (identifica pico,
  regressão após release).
- **Amostra de eventos** — últimas 10 ocorrências reais, cada uma
  linkando para a sessão que a originou.
- **Breadcrumbs** (últimos ~30 eventos antes do erro na sessão).
- **Snapshot do DOM** (se capturado) — reproduzido em iframe sandbox.
- **Comentários** — o time discute a triagem.
- **Botão "Atribuir"** — escolhe um usuário do time.
- **Botão "Alterar status"** — open → investigating → resolved / ignored.

### 5.3. Regressão automática

Se uma issue marcada como `resolved` volta a ocorrer após um deploy
(release novo), o sistema **detecta a regressão automaticamente** e
reabre a issue com uma nota no timeline.

---

## 6. Módulo: Releases

**Rota:** `/releases`

**O que mostra:** cada release (versão) do aplicativo que foi vista
capturando eventos, com métricas agregadas:

- Versão (ex.: `1.2.3`).
- Data primeira/última vez vista.
- Total de sessões nessa release.
- Total de erros.
- Taxa de erro.
- Novos erros (fingerprints que só apareceram nessa versão).
- p95 de latência.

**Comparação entre releases:**
- Selecione 2 releases → o sistema mostra o **delta** de:
  - Taxa de erro (%).
  - p95 de latência (ms).
  - Novos erros introduzidos.
  - Erros que sumiram (possivelmente corrigidos).

**Casos de uso:**
- Após deploy: "essa release piorou algo? Vou comparar com a anterior".
- Post-mortem: "quando essa taxa de erro começou a subir?".

---

## 7. Módulo: Performance (Web Vitals)

**Rota:** `/performance`

**Métricas coletadas** (automaticamente pelo SDK, sem código no app):

- **LCP** (Largest Contentful Paint) — tempo até o maior elemento
  visual renderizar. Bom: < 2.5s; Ruim: > 4s.
- **CLS** (Cumulative Layout Shift) — deslocamentos indesejados de
  layout. Bom: < 0.1; Ruim: > 0.25.
- **INP** (Interaction to Next Paint) — responsividade em cliques.
  Bom: < 200ms; Ruim: > 500ms.
- **FID** (First Input Delay) — atraso do primeiro clique.
  Bom: < 100ms; Ruim: > 300ms.
- **TTFB** (Time to First Byte) — tempo até o primeiro byte da
  resposta. Bom: < 800ms; Ruim: > 1.8s.
- **FCP** (First Contentful Paint) — primeiro conteúdo desenhado.

**O que a tela mostra:**
- Para cada métrica: distribuição p75 e p95 por rota (`screen`).
- Percentual "good / needs-improvement / poor" por rota.
- Filtro por app, período, screen.

**Cenário típico:** o time de UX abre depois do deploy e vê que o LCP
da tela de checkout piorou de 1.8s (good) para 3.4s (needs-improvement)
— sabe imediatamente que precisa investigar.

---

## 8. Módulo: Funis

**Rota:** `/funnels`

Um funil é uma **sequência de passos** que o usuário deve percorrer, e
o sistema calcula a taxa de conversão entre cada passo.

### 8.1. Definir um funil

O admin cria um funil informando:
- Nome (ex.: "Onboarding B2C").
- App emissor.
- Passos, cada um sendo um filtro sobre eventos:
  - Tipo (`page_view`, `action`, `http_request`, `error`, `custom`).
  - Nome (ex.: `tela_cadastro`, `clicou_confirmar`).
  - Feature (opcional).
  - Screen (opcional).
- Janela de tempo (ex.: passos devem acontecer no mesmo dia; ou em
  até 30 minutos).

### 8.2. Executar um funil

Ao selecionar um funil e um período:

- Tabela com cada passo, mostrando:
  - Quantos usuários únicos chegaram ao passo.
  - Taxa de conversão em relação ao passo anterior.
  - Taxa de drop-off (quantos abandonaram).
- Gráfico de funil visual (barras decrescentes).

**Cenário típico:** o PM cria um funil de "cadastro → confirmação email
→ primeira compra" e monitora semanalmente. Se a conversão do passo 2
para o 3 cai, ele investiga aquela etapa especificamente.

---

## 9. Módulo: Retenção

**Rota:** `/retention`

Mostra **cohorts de retenção** — de todos os usuários que fizeram algo
na semana N, quantos voltaram nas semanas seguintes?

**Tabela de cohort (heatmap):**
- Linhas: cohort semanal (ex.: "semana de 5-ago").
- Colunas: D1, D3, D7, D14, D30 após o primeiro evento.
- Célula: % de usuários da cohort que voltou naquele dia.

**Configurável:** o admin escolhe o "evento de entrada" (o que caracteriza
o primeiro evento) e o "evento de retenção" (o que caracteriza retornar).

**Cenário típico:** PM avalia se a última mudança de UX aumentou
retenção D7 — compara cohort da semana pré-mudança vs. pós-mudança.

---

## 10. Módulo: Explorador de eventos

**Rota:** `/traces`

Ferramenta de investigação livre — mostra a lista bruta de eventos
capturados, com filtros combináveis.

**Filtros disponíveis:**
- Período (from/to).
- App emissor.
- Tipo do evento.
- Nome.
- Feature.
- Screen (rota do frontend).
- User ID (rastreia jornada de um usuário específico).
- Session ID (foca em uma sessão).
- Route (URL da API chamada).
- Status HTTP (min/max).
- Only errors (apenas `type=error`).
- Busca full-text (mensagem, corpo do request/response).

**Colunas da lista:**
- Timestamp.
- App.
- Tipo (ícone).
- Nome / mensagem.
- Screen.
- HTTP método + URL + status.
- Duração (ms).
- Session ID (linkável).
- User ID (linkável).

**Expansão de linha:**
- Ao clicar em uma linha, expande mostrando:
  - Payload de request/response (formatado, JSON collapsível).
  - Metadata completa.
  - Stack trace (se erro).
  - Snapshot do DOM (se erro com snapshot).
  - Trace ID + Span ID (correlação com backend via W3C traceparent).

**Casos de uso:**
- "O cliente X reclamou às 14h32 — o que ele fez?".
- "Quais eventos vieram do IP Y na última hora?".
- "Todas as requisições `POST /pagamento` com status 500 hoje".

---

## 11. Módulo: Sessões

**Rota:** `/sessions` (lista) e `/sessions/{sessionId}` (timeline).

### 11.1. Lista de sessões

- **Colunas:** session ID, primeiro evento, último evento, duração
  total, quantos eventos, app, user ID.
- **Filtros:** período, app, user ID, com erros / sem erros.
- **Casos de uso:** encontrar todas as sessões de um usuário, todas as
  sessões com erro em uma feature específica.

### 11.2. Timeline de sessão

Ao abrir uma sessão, a tela mostra a **jornada completa** do usuário:

- **Cabeçalho:** duração, quantidade de eventos, apps envolvidas.
- **Timeline vertical:** cada evento em ordem cronológica, com ícone por
  tipo (📄 page view, 👆 action, 🌐 HTTP request, ⚠️ error, ⭐ custom).
- **Cada evento expandível:** payload completo.
- **Marca de erro:** eventos de erro destacados em vermelho.
- **Se há snapshot** de algum erro, é possível abrir o replay ali mesmo.

**Casos de uso:**
- Suporte reproduz a experiência do cliente que ligou.
- Time de produto entende jornadas anômalas ("por que essa sessão levou
  47 minutos e nenhum evento de conversão?").

---

## 12. Módulo: Session replay

**O que é:** captura visual do DOM no momento do erro, exibida em iframe
sandbox no dashboard. Não é vídeo contínuo (MVP) — é foto do estado no
momento exato.

**Como aparece:**
- No detalhe da issue: botão "Ver DOM no momento do erro".
- No detalhe do evento no explorador: mesma opção.

**O que mostra:**
- HTML sanitizado (scripts removidos, `<iframe>` removidos).
- Inputs sensíveis mascarados automaticamente (password, email, tel,
  cartão, CVV).
- Elementos marcados com `data-ndovu-mask` têm texto substituído por
  `***`.
- Resolução original (viewport width/height gravados).

**Limitação atual:** é snapshot único, não sequência. Evolução prevista
para gravação contínua tipo vídeo (rrweb).

---

## 13. Administração: Empresas

**Rota:** `/admin/companies`

**Para quê:** o ndovu é multi-tenant leve — pode servir várias empresas
(clientes ou unidades de negócio) na mesma instância, com isolamento
completo de dados.

**Operações:**
- Listar empresas.
- Criar empresa (nome + documento opcional + ativa).
- Editar.
- Desativar (mantém dados históricos, mas usuários da empresa não
  conseguem mais logar).
- Excluir (cuidado — apaga em cascata).

**Regras:**
- Toda app pertence a uma empresa.
- Todo usuário pertence a uma empresa.
- Viewer/Admin normal só enxerga dados da sua empresa.
- Super-admin transita entre empresas.

---

## 14. Administração: Apps

**Rota:** `/admin/apps`

Um "app" no ndovu é **um emissor de eventos** — cada frontend, mobile
ou API interna que instrumenta o SDK.

**Operações:**
- Listar apps da empresa.
- Cadastrar app (nome único + tecnologia + empresa + responsável).
- Editar.
- Excluir.

**Ao criar um app:** o sistema **gera automaticamente uma chave de API**
para aquele app, exibida uma única vez na tela. Essa chave é o que o
SDK usa para autenticar.

**Restrição importante:** o SDK envia `app` no envelope do evento; o
sistema valida que o `app` bate com a chave usada. Impede que uma app
"se passe" por outra por engano.

**Regras:**
- Nome do app deve ser único dentro da empresa.
- Ao deletar app, os eventos históricos ficam preservados no ClickHouse
  (o TTL cuida depois).

---

## 15. Administração: Usuários e permissões

**Rota:** `/admin/users`

### 15.1. CRUD de usuários

**Operações:**
- Listar usuários da empresa.
- Cadastrar (nome, email, senha inicial, role, empresa, ativo).
- Editar (alterar role, ativar/desativar, mudar de empresa).
- Trocar senha (auto ou por admin).

**Roles disponíveis:**
- **admin** — controle total sobre a empresa.
- **viewer** — leitura + comentários.
- **super** (apenas configurável no banco, não pela UI por segurança).

### 15.2. RBAC granular

Um viewer pode ter permissões explícitas concedidas a apps específicos:

- Na página do usuário, há um painel "Permissões por app".
- Admin concede/revoga acesso a cada app individualmente.
- Se o viewer não tem nenhuma permissão explícita, vê todas as apps da
  empresa (comportamento padrão).
- Se tem pelo menos uma permissão explícita, o sistema entende que é
  "acesso restrito" — vê **apenas** as apps concedidas.

**Cenário:** analista do time "Cobrança" recebe acesso a
`portal-cobranca` e `app-mobile-cobranca`. Nunca vê o `portal-vendas`
da mesma empresa.

### 15.3. Salvaguarda

- Não é possível desativar o último admin ativo — o sistema retorna erro.
- Um usuário não pode se auto-desativar.
- Todas as ações ficam no audit log.

---

## 16. Administração: Chaves de API

**Rota:** `/admin/keys`

**Para quê:** cada app tem 1 ou mais chaves. Cada SDK precisa de uma
chave para enviar eventos. Chaves são hasheadas no banco (nunca
expostas em texto claro após criação).

**Operações:**
- Listar chaves (mostra prefixo + últimos 4 caracteres, nunca a chave
  completa).
- Criar chave nova (para rotacionar).
- Revogar chave (efeito em até 30s — cache).

**Boas práticas exibidas na UI:**
- Rotacionar chave a cada 90 dias.
- Ao revogar, gerar nova antes de descomissionar a antiga (grace period
  onde as duas funcionam).

---

## 17. Administração: Alertas

**Rota:** `/admin/alerts`

**O que é:** regras que disparam notificação quando uma condição é
atingida.

**Estrutura de uma regra de alerta:**
- Nome (ex.: "5xx no checkout").
- App (ou "todas").
- Código de erro (ex.: `NETWORK_ERROR`, `HTTP_500`, ou vazio = qualquer erro).
- Threshold (X ocorrências).
- Janela (em Y segundos).
- Canal: **Slack** (webhook) ou **Webhook genérico**.
- URL de destino.
- Silêncio (não repetir alerta por Z segundos após disparar).
- Ativa / inativa.

**Avaliação:** o sistema verifica cada regra a cada 60 segundos.

**Payload enviado (JSON):**
- Rule name.
- Condition.
- Sample eventId + link para o dashboard.
- Contagem de ocorrências.
- Últimos usuários afetados.

**Cenário:** "novo erro nunca visto no checkout → me avise no Slack".

---

## 18. Administração: Anomalias

**Rota:** `/admin/anomalies`

**Diferença para alertas:** enquanto alertas usam threshold fixo ("mais
de 10 erros em 5 minutos"), anomalias detectam **desvios estatísticos**
comparados ao comportamento histórico. O sistema aprende o padrão
normal e avisa quando algo sai fora.

### 18.1. Regras de anomalia

**Estrutura:**
- Nome.
- App (ou vazio = todas).
- Métrica:
  - `error_count` — quantidade absoluta de erros.
  - `event_count` — quantidade total de eventos.
  - `error_rate` — proporção de erros por evento.
- Janela atual (minutos analisados).
- Baseline (quantas semanas para trás formam a "referência").
- Sensibilidade (em desvios-padrão σ). Menor = mais sensível.
- Direção: `above` (spike), `below` (silêncio suspeito), `both`.
- Silêncio pós-disparo.
- Canal + URL.
- Ativa.

**Como funciona:** o sistema compara a janela atual com a mesma janela
(mesma hora + mesmo dia da semana) nas últimas N semanas. Calcula a
média e o desvio-padrão dessas semanas. Se o valor atual desvia mais
que `sensitivity × σ`, dispara.

**Vantagem sobre threshold fixo:** sensível a padrões sazonais. Se
segunda-feira 10h costuma ter mais tráfego, o sistema entende — não
dispara falso positivo.

### 18.2. Detecções recentes

Tabela com histórico de disparos:
- Quando aconteceu.
- Qual regra.
- Direção.
- Valor atual vs. baseline (média ± σ).
- Z-score.
- Se o alerta foi entregue com sucesso.

**Cenário típico:** "avisar quando taxa de erro no login desviar 3σ
para cima" — captura ataques de brute-force, quebras de dependência,
regressões pós-deploy.

---

## 19. Administração: Sampling

**Rota:** `/admin/sampling`

**Para quê:** controlar o volume de eventos armazenados. Se um app
gera 100M eventos/mês e a maioria é `page_view` de baixo valor, o
sampling descarta parte deles antes de gravar — reduzindo custo sem
perder o essencial.

**Regras:**
- App (ou vazio = todas).
- Tipo do evento (ou vazio = todos).
- Sample rate (0.0 a 1.0 — proporção que **fica**).
- Keep errors (booleano) — se true, **erros nunca são descartados**
  mesmo com sample_rate baixo.
- Ativa.
- Nota (livre, para documentação).

**Precedência de regras:**
1. app + type específicos.
2. app específico + type vazio.
3. app vazio + type específico.
4. app vazio + type vazio (global).

**Cenário:** "guardar 100% dos erros, 50% das page_views, 100% do
resto" — reduz volume em ~30-40% sem impacto em investigação.

---

## 20. Administração: Source Maps

**Rota:** `/admin/source-maps`

**Para quê:** código JavaScript em produção é minificado — stack traces
mostram nomes ilegíveis (`a.b.c`). Source maps permitem "desminificar"
e mostrar o código original.

**Operações:**
- Upload de arquivo `.map` (associado a app + release + filename).
- Listar source maps (por app / release).
- Deletar.

**Como funciona:** quando um erro chega com stack trace, o sistema
tenta encontrar um source map correspondente (mesma app + mesma
release). Se acha, resolve as linhas do stack para o arquivo original
+ linha original.

**Onde aparece:** na tela de detalhe da issue e na expansão de um evento
de erro no explorador, o stack aparece já resolvido.

---

## 21. Administração: Feedback do usuário

**Rota:** `/admin/feedbacks`

**Para quê:** o SDK oferece um widget flutuante ("Feedback") que os
usuários finais dos apps podem clicar para reportar problemas,
sugestões ou elogios diretamente do produto. Cada feedback é atrelado
à sessão e ao último eventId — facilita o time reproduzir o contexto.

### 21.1. Como aparece para o usuário final

Um botão discreto no canto inferior da tela. Ao clicar, abre modal com:
- Tipo (bug, sugestão, elogio, outro).
- Email (opcional).
- Mensagem (até 5000 caracteres).

### 21.2. Como aparece para o admin

Lista de cards, cada um mostrando:
- Ícone do tipo (🐞 bug, 💡 sugestão, ❤ elogio).
- App emissor.
- Status (novo, em triagem, resolvido, descartado).
- Data de envio.
- Mensagem completa.
- Email do reportador (se informado).
- URL onde estava.
- Viewport (resolução).
- **Link "ver sessão + timeline"** — pula direto para reproduzir a
  jornada.

**Filtros:** por status, por app.

**Ações:**
- Marcar como em triagem.
- Marcar como resolvido.
- Descartar.
- Remover.

**Cenário:** usuário clica "Feedback" após tentar exportar relatório e
falhar. O time recebe, clica em "ver sessão", assiste todos os eventos
prévios e o snapshot do DOM no momento — resolve em minutos.

---

## 22. Administração: Audit Log

**Rota:** `/admin/audit-log`

**O que registra:** todas as ações administrativas — quem fez, o quê,
quando, onde.

**Ações típicas capturadas:**
- Login / logout.
- Criar / editar / apagar usuário.
- Criar / revogar chave.
- Criar / editar / apagar app.
- Criar / editar / apagar empresa.
- Alterar permissões.
- Executar operações LGPD (export / delete).
- Configurar alertas / anomalias / sampling.
- Ações em feedbacks (mudar status, deletar).

**Colunas:**
- Timestamp.
- Ator (email + user ID).
- Ação (verbo curto: `user.create`, `key.revoke`, `gdpr.forget`).
- Recurso (tipo + ID).
- IP.
- User agent.
- Detalhes (JSON com o que mudou).

**Filtros:** período, ator, ação, tipo de recurso.

**Garantia:** insert-only. Nem admin consegue editar ou apagar entradas
do audit log (o próprio produto não expõe endpoint para isso).

**Cenário:** ANPD solicita comprovação de quem exportou dados do titular
X em determinada data. Filtra `action=gdpr.export` + período — resposta
em segundos.

---

## 23. Administração: LGPD

**Rota:** `/admin/gdpr`

Ferramenta dedicada aos direitos do titular sob a LGPD.

### 23.1. Exportar dados de um usuário

- Admin informa o `user_id`.
- Sistema retorna JSON com **todos os eventos** capturados sobre
  aquele usuário (independentemente de app, período, tipo).
- Arquivo baixável, formato aberto (JSON).

**Uso:** atender solicitação de acesso do titular (LGPD Art. 18, II).

### 23.2. Direito ao esquecimento

- Admin informa o `user_id`.
- Sistema executa `ALTER DELETE` no ClickHouse, removendo **todos** os
  eventos daquele usuário (não é soft delete — apagamento efetivo).
- A operação fica no audit log.

**Uso:** atender solicitação de eliminação do titular (LGPD Art. 18, VI).

### 23.3. Retenção automática

Independente de ação manual, todos os eventos têm TTL de 90 dias por
default (configurável). Após 90 dias, o ClickHouse apaga automaticamente
via `TTL` de tabela.

---

## 24. Funcionalidades do SDK visíveis ao usuário final

Embora o SDK seja código, algumas funcionalidades geram artefatos que o
usuário final do app **vê ou percebe**:

### 24.1. Widget de feedback (opcional)

Se a app monta o widget (`sdk.mountFeedbackWidget()`), o usuário vê um
botão flutuante e pode enviar feedback. É o único elemento visível.

### 24.2. Nenhum outro impacto visual

- SDK roda em background.
- Sem overlays, sem popups, sem tracking pixels.
- Sem cookies persistentes (só `sessionStorage` para manter session ID
  durante reloads da mesma aba).

### 24.3. Impacto de performance

- Payload: ~15 KB gzipped.
- Latência adicional em requisições: **zero** (SDK envia em background,
  fire-and-forget).
- Consumo de CPU: negligible (ring buffer + fetch enqueue).

### 24.4. Privacidade

- SDK **redige automaticamente** campos sensíveis antes de sair do
  navegador: `password`, `senha`, `token`, `secret`, `authorization`,
  `cvv`, `card`, `cartão`.
- Snapshots do DOM mascaram inputs sensíveis e qualquer elemento marcado
  com `data-ndovu-mask`.

---

## 25. Fluxos ponta-a-ponta

### 25.1. Fluxo: novo desenvolvedor entra no time

1. Admin cadastra usuário em `/admin/users` (email, senha temporária,
   role viewer).
2. Se necessário, concede permissões granulares a apps específicos.
3. Envia credencial ao dev pelo canal seguro.
4. Dev acessa `/login`, troca senha, começa a explorar.

### 25.2. Fluxo: novo app entra em produção

1. Admin cadastra app em `/admin/apps` (nome, tecnologia, empresa,
   responsável).
2. Sistema gera chave de API — admin copia (exibida 1x).
3. Dev instala o SDK, configura endpoint + chave + nome do app.
4. Deploy do app.
5. Eventos começam a aparecer no `/traces` em segundos.
6. Se erros críticos surgirem, admin cria uma regra de alerta em
   `/admin/alerts`.

### 25.3. Fluxo: incidente em produção

1. Alerta chega no Slack: "5 erros de `NETWORK_ERROR` em 30s no
   portal-cliente".
2. Dev abre o link do alerta → chega em `/issues/{fingerprint}`.
3. Vê stack trace desminificado + snapshot do DOM + breadcrumbs.
4. Comenta atribuindo o problema a Fulano.
5. Fulano investiga, corrige, faz deploy.
6. Marca a issue como `resolved`.
7. Se voltar a ocorrer, sistema reabre automaticamente + notifica.

### 25.4. Fluxo: análise de conversão

1. PM cria funil em `/funnels` (cadastro → confirmação → primeira
   compra).
2. Executa para "últimos 7 dias".
3. Identifica queda no passo 2→3.
4. Vai em `/traces`, filtra por `screen=/confirmacao` e "com erros" no
   mesmo período.
5. Descobre que a rota `POST /confirmar` está retornando 503
   intermitentemente.
6. Aciona time de backend com evidência exata (timestamps, payloads,
   usuários afetados).

### 25.5. Fluxo: solicitação de LGPD

1. Titular envia solicitação ao DPO.
2. DPO abre `/admin/gdpr`.
3. Executa export → salva JSON, envia ao titular.
4. Executa delete → confirma na UI, recebe count de eventos apagados.
5. Audit log registra as duas ações com timestamp + IP.
6. DPO gera comprovante do audit log para arquivo.

### 25.6. Fluxo: rotação de chave suspeita

1. Admin percebe uso anômalo (via digest ou alerta externo).
2. Vai em `/admin/keys`.
3. Cria uma nova chave para o mesmo app.
4. Comunica ao dev responsável a troca.
5. Dev atualiza a config do SDK no app, faz deploy.
6. Admin volta em `/admin/keys` e revoga a antiga.
7. Cache do backend expira em até 30s — chave antiga para de funcionar.
8. Audit log registra as três ações (create, revoke).

---

## 26. Notificações e digests

### 26.1. Alertas em tempo real

Disparados por **regras de alerta** (threshold) ou **regras de
anomalia** (estatístico). Canais suportados:

- **Slack** (via incoming webhook).
- **Webhook genérico** (qualquer endpoint HTTP que aceite POST JSON).

O payload é padronizado e inclui link direto para o dashboard.

### 26.2. Digest semanal por email

Enviado automaticamente aos destinatários configurados. Default:
domingo às 20h UTC (configurável por dia da semana e hora).

**Conteúdo do email:**
- Top 10 erros da semana com Impact Score.
- Novos erros que apareceram (fingerprints inéditos).
- Web Vitals médios por app.
- Anomalias detectadas.
- Feedbacks negativos abertos.
- Comparativo com a semana anterior (Δ%).

**Backend de envio:** SendGrid (se configurada API key) ou SMTP direto
(MailHog em dev, servidor real em produção). O sistema detecta
automaticamente qual usar.

**Pode ser forçado imediatamente** via botão "Enviar agora" na página
de admin (útil para testar configuração de email).

---

## Apêndices

### A.1. Glossário

- **App emissor**: uma aplicação (site, mobile, API) que instala o SDK
  e envia eventos.
- **Chave de API**: token de autenticação que o SDK usa. Uma app pode
  ter múltiplas chaves.
- **Empresa (tenant)**: agrupador administrativo. Isola dados entre
  clientes ou unidades de negócio.
- **Evento**: uma ocorrência capturada — page_view, action, http_request,
  error, custom.
- **Fingerprint**: hash que identifica um erro único (para agrupar
  ocorrências).
- **Impact Score**: score composto que ranqueia issues por severidade.
- **Issue**: erro agrupado por fingerprint. Tem status (open/investigating/
  resolved/ignored) e assignee.
- **Release**: versão do app (ex.: `1.2.3`). Enviada pelo SDK.
- **Sessão**: conjunto de eventos de um usuário em uma visita.
- **Session replay**: reprodução visual do DOM no momento do erro.
- **Snapshot**: HTML sanitizado da tela no momento em que um erro foi
  capturado.
- **Source map**: arquivo que permite desminificar código JavaScript.
- **Trace ID / Span ID**: identificadores W3C que correlacionam a
  requisição do frontend com o backend (via traceparent).
- **User feedback**: mensagem enviada pelo usuário final via widget do
  SDK.
- **Web Vitals**: métricas padronizadas do Google para performance de
  frontend.

### A.2. Documentação complementar

- **[EXECUTIVA.md](EXECUTIVA.md)** — visão de valor para liderança.
- **[TECNICA.md](TECNICA.md)** — para devs que vão integrar / manter.
- **[ARQUITETURA.md](ARQUITETURA.md)** — decisões de design.
- **[INTEGRATION.md](INTEGRATION.md)** — walkthrough de integração de
  um novo app.
- **[CONTRACT.md](CONTRACT.md)** — spec do contrato de ingestão v1.

---

Documento vivo. Última atualização: agosto/2026.
Cobre o produto após a Fase 4 (Diferenciais Avançados) concluída.
