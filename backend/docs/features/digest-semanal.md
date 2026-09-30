# Digest semanal (scheduler + e-mail)

## Para que serve

Manda um e-mail toda semana com o **resumo dos últimos 7 dias**:
totais (eventos, sessões, usuários, erros), top issues abertas e
releases ativos. É a forma passiva de manter time e stakeholders no
loop sem precisar abrir o dashboard.

Roda no processo do **writer** (não no api) como um scheduler
próprio; testável imediatamente via endpoint admin.

## Onde fica

- Serviço: `backend/internal/usecase/digests.go` (`DigestService`).
- Loop: `DigestService.Run(ctx, interval)` — bloqueia; tick default
  a cada 5min avaliando janela de agendamento.
- Endpoint admin (send-now): `POST /v1/admin/digest/send-now`
  (auth Bearer + `requireRole(admin)`).
- Handler: `backend/internal/adapter/httpapi/handlers.go` →
  `PostDigestSendNow`.
- Mailer factory: `backend/internal/platform/mailer_factory.go`.
- Implementações: `mailer.go` (SMTP), `mailer_sendgrid.go` (REST v3),
  noop (default sem config).
- Env vars:
  - `NDOVU_DIGEST_RECIPIENTS` — CSV de e-mails (**sem isso, o loop
    vira no-op**).
  - `NDOVU_DIGEST_WEEKDAY` — 0=domingo (default), 1=segunda, ...
  - `NDOVU_DIGEST_HOUR_UTC` — hora do envio (default `20`).
  - `NDOVU_DIGEST_MINUTE_UTC` — minuto (default `0`).
  - `NDOVU_DIGEST_TICK_SECONDS` — periodicidade do tick (default 300).
  - `NDOVU_DIGEST_DASHBOARD_URL` — usado nos links do e-mail
    (default `http://localhost:13000`).
  - `NDOVU_MAILER_PROVIDER` — `auto|sendgrid|smtp|noop`. `auto`
    prefere SendGrid se `NDOVU_SENDGRID_API_KEY` estiver setado,
    senão SMTP se `NDOVU_SMTP_HOST` estiver setado, senão noop.
  - SMTP: `NDOVU_SMTP_HOST`, `NDOVU_SMTP_PORT` (default 1025 para
    MailHog), `NDOVU_SMTP_USER`, `NDOVU_SMTP_PASSWORD`,
    `NDOVU_SMTP_FROM`.
  - SendGrid: `NDOVU_SENDGRID_API_KEY` (+ `NDOVU_SMTP_FROM`
    compartilhado como remetente).

## Como usar

**Dev local com MailHog** (docker-compose do repo sobe MailHog em
`localhost:1025` SMTP, `localhost:18025` UI web):

```bash
export NDOVU_DIGEST_RECIPIENTS='dev@ndovu.local,pm@ndovu.local'
export NDOVU_SMTP_HOST=localhost
export NDOVU_SMTP_PORT=1025
export NDOVU_SMTP_FROM='ndovu@ndovu.local'
# provider auto: sem SENDGRID_API_KEY, cai em SMTP
```

Sobe o writer. Para não esperar até domingo às 20:00 UTC, dispare
imediatamente:

```bash
TOKEN="eyJ..."   # token admin
curl -sS -X POST http://localhost:18081/v1/admin/digest/send-now \
  -H "Authorization: Bearer $TOKEN"
```

Resposta:

```json
{"sent": true}
```

Abra `http://localhost:18025` — o e-mail aparece na UI do MailHog
com HTML + texto plano.

**Produção com SendGrid** (auto detecta):

```bash
export NDOVU_SENDGRID_API_KEY='SG.xxxxx'
export NDOVU_SMTP_FROM='no-reply@suaempresa.com'
export NDOVU_DIGEST_RECIPIENTS='time@suaempresa.com'
export NDOVU_DIGEST_DASHBOARD_URL='https://ndovu.suaempresa.com'
export NDOVU_DIGEST_WEEKDAY=1     # segunda
export NDOVU_DIGEST_HOUR_UTC=12
```

## O que acontece

**Scheduler (`DigestService.Run`)**
- Se `recipients` está vazio ou mailer é nil, loga
  `"digest semanal desligado"` e retorna — o loop vira no-op.
- Cria um `ticker` (`NDOVU_DIGEST_TICK_SECONDS`, default 5min).
- Cada tick, `tick()` avalia:
  - `now.Weekday() != s.weekday` → skip.
  - Hora atual antes de `hourUTC:minuteUTC` → skip.
  - `lastSent` a menos de 24h → skip (idempotência intra-processo:
    reinício no mesmo dia não duplica).
- Passou nos três: chama `SendOnce`.
- Faz um tick imediato no start (`s.tick(ctx)`) — catch-up se o
  processo reiniciou depois da janela do dia.

**Envio (`SendOnce`)**
1. `end = now.UTC()`, `start = end - 7 dias`.
2. Consulta `EventReader.GetOverview(start, end, "")` — totais.
3. Consulta `IssueService.List(...)` com `OnlyOpen=true`, `limit 10`.
4. Consulta `FindReleases(...)` com `limit 5`.
5. Renderiza HTML minimalista (estilos inline; sem CSS externo pois
   Gmail/Outlook bloqueiam) + fallback `text/plain`.
6. `mailer.Send(from, recipients, subject, htmlBody, textBody)`.
7. Loga `enviando digest` com contadores.
8. Marca `lastSent = now`.

**Conteúdo do e-mail**
- Subject: `Ndovu — digest semanal (DD/MM → DD/MM)`.
- 4 tiles: Eventos, Sessões, Usuários, Erros (erros em vermelho se >0).
- Top 10 issues abertas com fingerprint, código, contagem e usuários
  afetados; link direto pra `/issues/{fingerprint}` no dashboard.
- Releases ativos (até 5): app, versão, eventos, % erro (>5% em
  vermelho).
- Fallback texto plano com os mesmos dados.

**Mailer factory (`NewMailer`)**
- `sendgrid` explícito com key → `SendGridMailer` (POST
  `https://api.sendgrid.com/v3/mail/send`).
- `smtp` explícito com host + from → `SMTPMailer` (STARTTLS opcional;
  auth só se `Username != ""`, para MailHog funcionar sem creds).
- `noop` explícito → só loga.
- `""` ou `auto`: SendGrid se `SENDGRID_API_KEY`, senão SMTP se
  `SMTP_HOST+FROM`, senão noop.
- Provider desconhecido → noop com warning.

## Como demonstrar

> "Toda segunda de manhã cai um resumo semanal no e-mail do time —
> quantos erros, quais issues abertas, quais releases estão sangrando.
> Para demo, tem um `send-now` que dispara imediatamente e cai no
> MailHog local em segundos."

Roteiro de 60s:
1. Rode `docker compose up mailhog` (se não estiver ativo). Abra
   `http://localhost:18025`.
2. Rode o `curl POST /v1/admin/digest/send-now` com token admin.
3. Volte no MailHog — mostre o e-mail chegando com HTML renderizado.
4. Mostre os links do e-mail apontando pra `/issues/<fingerprint>`
   no dashboard.
5. Fale sobre a factory: setar `SENDGRID_API_KEY` em prod troca o
   provider sem tocar em código; sem nada configurado, vira `noop`
   e o loop não quebra.

## Papéis (RBAC)

- **Scheduler** — roda no processo do writer, sem HTTP.
- **`send-now`** — auth Bearer + `requireRole(admin)`.
  Registra em audit log (`digest.send_now`). Não distingue super vs
  admin de company; a janela do digest é **global** (todos os apps
  de todas as companies) — por isso o padrão é usar em ambientes
  onde o time do super-admin é quem opera. Multi-tenant fino do
  digest é evolução futura.

## Dependências

- Writer rodando (`Run` só é chamado por ele, não pelo api).
- Mailer configurado (ou aceita cair em noop).
- `EventReader` (ClickHouse) e `IssueService` (Postgres) disponíveis
  — se qualquer um falha, `SendOnce` retorna erro e o loop tenta na
  janela seguinte.
- Em dev, MailHog no docker-compose (`localhost:1025` SMTP,
  `localhost:18025` UI).

## Perguntas frequentes

- **"Enviou duas vezes na mesma semana. Por quê?"** `lastSent` é
  em memória — reinício do writer perde o estado. Guard secundário
  é o `24h since lastSent`, então na mesma semana só dobraria se
  reiniciasse **depois** da hora do envio. Solução robusta:
  persistir `lastSent` (evolução) ou escalonar a hora do envio.
- **"Meu SMTP exige TLS."** `SMTPMailer` usa STARTTLS quando o
  servidor anuncia. Ajuste `NDOVU_SMTP_HOST` para o hostname real
  (não `localhost`) para o TLS handshake funcionar.
- **"Não recebi o e-mail."** Checar logs do writer: `mailer noop:
  envio ignorado` significa que caiu em noop (falta config). Também
  checar `NDOVU_DIGEST_RECIPIENTS` (CSV limpo, sem espaços).
- **"Posso mudar o layout?"** Sim, `DigestService.render` é HTML
  string puro — edite e recompile. Não tem template externo pra
  manter o binário auto-contido.
- **"Como testo o envio sem esperar o tick?"** `POST
  /v1/admin/digest/send-now` — dispara na hora ignorando
  agendamento (mas ainda respeita config de recipients/mailer).

## Referências

- Serviço: `backend/internal/usecase/digests.go` (`DigestService`,
  `Run`, `SendOnce`, `render`)
- Handler: `backend/internal/adapter/httpapi/handlers.go`
  (`PostDigestSendNow`)
- Mailer: `backend/internal/platform/mailer_factory.go`,
  `mailer.go` (SMTP), `mailer_sendgrid.go`
- Config: `backend/internal/config/config.go` (bloco Mailer + Digest)
- docker-compose: MailHog em `localhost:18025` (UI) e `1025` (SMTP)
- Docs relacionadas: [autenticacao.md](autenticacao.md),
  [visao-geral.md](visao-geral.md).
