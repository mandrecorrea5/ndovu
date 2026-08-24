# Autenticação (`/login` + JWT)

## Para que serve

Autentica quem opera o backoffice (dashboard) e emite o token que
todas as chamadas de consulta precisam apresentar. É a porta de
entrada para viewer, editor, admin e super-admin.

Ingestão de eventos **não** passa por aqui — SDKs enviam com
`X-Api-Key`. Login é só para pessoas.

## Onde fica

- Rota do dashboard: `/login`
- Página: `dashboard/src/app/login/page.tsx`
- Persistência do token no browser: `dashboard/src/lib/auth.ts`
- Endpoints:
  - `POST /v1/auth/login` — recebe email/senha, devolve JWT.
  - `GET  /v1/auth/me`   — identidade do token apresentado.
- Handler backend: `backend/internal/adapter/httpapi/admin_handlers.go`
  (`PostLogin`, `GetMe`).
- Emissão/verificação do token: `backend/internal/usecase/auth.go`
  (`AuthService.Login`, `AuthService.Verify`).
- Middleware do bearer: `backend/internal/adapter/httpapi/middleware.go`
  (`bearerAuth`) — protege todo `/v1/*` autenticado (login fica de fora).
- Env vars relevantes: `NDOVU_AUTH_SECRET`, `NDOVU_AUTH_TOKEN_TTL_HOURS`
  (default 8h), `NDOVU_ADMIN_EMAIL`, `NDOVU_ADMIN_PASSWORD`.

## Como usar

1. Suba o backend. No primeiro boot, se **não existe nenhum usuário**,
   o `AuthService.EnsureBootstrapAdmin` cria o admin inicial usando as
   env vars (default `admin@ndovu.local` / `admin12345`) e vincula à
   empresa `Padrão`.
2. Abra `http://localhost:13000/login`, informe as credenciais e
   entre — o dashboard chama `POST /v1/auth/login` e persiste o token
   em `localStorage` (`ndovu.token`) + o usuário (`ndovu.user`).
3. As telas seguintes carregam usando o token via header
   `Authorization: Bearer <jwt>`.

Login direto pela API (útil para curl/scripts):

```bash
curl -sS http://localhost:18081/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@ndovu.local","password":"admin12345"}'
```

Resposta:

```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expiresAt": "2026-08-25T00:15:32Z",
  "user": {
    "id": "usr_...", "email": "admin@ndovu.local", "name": "Administrador",
    "role": "admin", "companyId": "cmp_...", "active": true, "isSuper": false
  }
}
```

Uso do token:

```bash
TOKEN="eyJhbGciOi..."
curl -sS http://localhost:18081/v1/auth/me \
  -H "Authorization: Bearer $TOKEN"
```

## O que acontece

- `POST /v1/auth/login` compara a senha com o hash bcrypt do banco.
  Usuário inexistente e senha errada devolvem a mesma coisa (`401`) e
  gastam o mesmo tempo — evita enumeração.
- Senha OK: emite JWT HS256 assinado com `NDOVU_AUTH_SECRET`, claims
  `sub`, `email`, `name`, `role`, `companyId`, `isSuper`, `exp`, `iss=ndovu`.
- `GET /v1/auth/me` roda por trás do `bearerAuth`, que valida
  assinatura + `exp` e injeta a identidade no contexto. Devolve
  exatamente o que o dashboard usa para gate de UI (role, company).
- Usuário `active=false` recebe `401` mesmo com senha correta.
- Não há refresh token: quando o JWT expira, o próximo request
  devolve `401` e o dashboard redireciona para `/login?next=...`.

## Como demonstrar

> "A gente sobe o Ndovu e ele já vem com um admin. Login usa JWT
> assinado localmente, mas a verificação passa por uma interface —
> trocar por Keycloak/OIDC no futuro não muda nenhum handler."

Roteiro de 45s:
1. Abra o dashboard sem sessão — redireciona para `/login`.
2. Entre com `admin@ndovu.local` / `admin12345`.
3. Mostre a devtools → `localStorage.ndovu.token` (o JWT).
4. Rode `curl -H "Authorization: Bearer $TOKEN" .../v1/auth/me` no
   terminal, provando que o mesmo token vale para a API.
5. Comente que o TTL default é 8h (`NDOVU_AUTH_TOKEN_TTL_HOURS`) e
   que `NDOVU_AUTH_SECRET` **precisa trocar em produção**.

## Papéis (RBAC)

O endpoint devolve `role` do usuário; o dashboard usa isso pra
esconder botões, mas quem barra de verdade é o backend:

- **super-admin** (`isSuper=true`) — vê todas as companies.
- **admin** — CRUD de users/apps/keys/regras da própria company.
- **editor** — leitura + escrita colaborativa (funnels, issues).
- **viewer** — só leitura + saved views próprias.

Login não distingue papéis; só emite o token e devolve o `role` no
claim para os middlewares subsequentes (`requireRole`, `tenantScope`).

## Dependências

- Postgres (`NDOVU_POSTGRES_URL`) — tabela `users` com hash bcrypt.
- Empresa `Padrão` existente (migração `000004`) — sem ela o
  bootstrap do admin inicial falha.
- Env `NDOVU_AUTH_SECRET` idêntico entre api e writer (caso o writer
  precise validar tokens — não usa hoje, mas o serviço é o mesmo).

## Perguntas frequentes

- **"Como troco a senha do admin?"** Login como admin, PATCH em
  `/v1/admin/users/{id}` com `{"password":"nova-senha->=8"}`. Ou
  reinicie com `NDOVU_ADMIN_PASSWORD` diferente **antes** de existir
  qualquer user — depois o bootstrap não roda mais.
- **"O token expirou no meio de uma sessão longa. E daí?"** A
  próxima chamada devolve `401` e o dashboard manda pra `/login`
  preservando a rota via `?next=`.
- **"Posso usar SSO?"** Sim, sem tocar em handler: implemente
  `domain.TokenVerifier` (JWKS do IdP) e injete no `bearerAuth` no
  lugar do `AuthService`.
- **"Onde ficam as tentativas falhas de login?"** No log estruturado
  do api (nível info em sucesso, warn em falha). Auditoria explícita
  cobre ações após o login, não o login em si.

## Referências

- Página: `dashboard/src/app/login/page.tsx`
- Sessão no browser: `dashboard/src/lib/auth.ts`
- Handlers: `backend/internal/adapter/httpapi/admin_handlers.go`
  (`PostLogin`, `GetMe`)
- Serviço: `backend/internal/usecase/auth.go` (`AuthService`)
- Middleware: `backend/internal/adapter/httpapi/middleware.go`
  (`bearerAuth`)
- Config: `backend/internal/config/config.go` (bloco AuthSecret,
  AdminEmail, AdminPassword, KeyCacheTTL)
- Docs relacionadas: [ARQUITETURA.md](../ARQUITETURA.md),
  [admin-usuarios.md](admin-usuarios.md) (quando existir).
