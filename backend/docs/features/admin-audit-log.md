# Audit log (`/admin/audit-log`)

## Para que serve

Registro **imutável e insert-only** de tudo que admins fazem: criação
e alteração de usuários, empresas, apps, chaves, alertas, source
maps, envio de digest, triagem de issues, etc. Cada entrada traz
**quem** fez, **quando**, **qual ação**, **qual recurso**, **de qual
IP** e o **user-agent**. É o traço frio de conformidade que sustenta
LGPD/GDPR, ISO 27001 e post-mortem de incidente de acesso.

## Onde fica

- Rota: `/admin/audit-log`
- Arquivo: `dashboard/src/app/admin/audit-log/page.tsx`
- Endpoint: `GET /v1/admin/audit-log?actor=&action=&resourceType=&from=&to=&limit=&offset=`
- Papel mínimo: **admin**

## Como usar

1. Abra **Audit log**. A tabela mostra 50 entradas por página, mais
   recentes primeiro.
2. Filtros:
   - **Ação** — dropdown com quick pick (user.create, user.update,
     app.create, app.update, app.delete, company.create,
     company.update, company.delete, apikey.create, apikey.revoke,
     alert.create, alert.delete, sourcemap.upload, sourcemap.delete,
     issue.update, digest.send_now).
   - **Actor userId** — cole o UUID do usuário que executou (útil ao
     rastrear atividade de um admin específico).
3. Clique **Consultar** para aplicar filtros (reseta paginação).
4. Botão **Atualizar** força re-fetch (útil quando algo acabou de ser
   feito em outra aba).
5. Use **← anterior** / **próxima →** no rodapé da tabela.

## O que você vê

- Cabeçalho:  
  "Registro imutável de ações administrativas — quem fez o quê,
  quando, com qual IP. Insert-only por design."
- Filtros em um card no topo.
- Tabela: **Quando**, **Quem** (e-mail + user-agent truncado),
  **Ação** (badge monospace em destaque), **Recurso** (tipo + id),
  **Detalhes** (JSON compacto do payload), **IP**.
- Rodapé com paginação: `1–50 de N`.
- Estado vazio: "Nenhuma entrada com esse filtro."

## Como demonstrar

> "O compliance officer pediu: 'quem deletou o app portal-checkout
> ontem?'. Vou em Audit log, filtro por ação `app.delete`, acho a
> linha, vejo quem, IP, user-agent e o JSON com os detalhes. É o
> registro que a auditoria externa vai pedir e o que salva o time
> quando alguém aponta o dedo errado."

Roteiro de 60s:
1. Vá em `/admin/apps` e delete um app de teste.
2. Volte no audit log, aponte a nova linha no topo com
   `app.delete` em destaque.
3. Aponte a coluna **Detalhes** — JSON com o app deletado.
4. Filtre por **Ação** = `apikey.revoke` — mostre revogações
   anteriores.
5. Filtre por **Actor userId** de outro admin — mostre a paginação
   funcionando.

## Papéis (RBAC)

- **admin only**. Editor/viewer não veem o link.
- Admin só vê entradas do próprio tenant. Ações que afetam recursos
  de A não vazam para admin de B.
- `is_super` vê o audit log inteiro (cross-tenant) — usado para
  auditoria plataforma-wide.
- **Nenhum papel pode editar ou deletar entradas** — insert-only por
  design. A imutabilidade é a garantia forense.

## Dependências

- Toda mutação admin no backend chama o `auditLogger` — se um
  handler novo esquecer de logar, aparece um buraco. Convenção:
  qualquer novo endpoint `POST/PATCH/DELETE` em `/v1/admin/*` deve
  registrar.
- Ações do GDPR (`export`/`forget`) também caem aqui, pelo bem da
  cadeia de custódia legal.

## Perguntas frequentes

- **"Consigo apagar uma entrada?"**  
  Não. Nem pela UI nem pela API. Tabela é insert-only e ainda serve
  de defesa em caso de perícia.
- **"Como filtro por período?"**  
  A API aceita `from` e `to`, mas a UI atual só expõe `action` e
  `actor`. Use `curl` para janelas específicas.
- **"Quanto tempo fica armazenado?"**  
  Até você limpar manualmente. Não há retenção automática — é
  intencional para atender LGPD (art. 15).
- **"O JSON de Detalhes vaza dado sensível?"**  
  Ações que envolvem senha registram só que a senha foi trocada, sem
  o valor. Chaves de API guardam prefixo, não a chave em claro.

## Referências

- Página: `dashboard/src/app/admin/audit-log/page.tsx`
- API client: `dashboard/src/lib/api.ts` → `auditLog`
- Registro backend: `backend/internal/adapter/audit` (chamado por
  todos os handlers admin)
- Docs relacionadas: [admin-users.md](admin-users.md),
  [admin-gdpr.md](admin-gdpr.md), [admin-keys.md](admin-keys.md)
