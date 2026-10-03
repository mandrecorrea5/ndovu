# Chaves de API (`/admin/keys`)

## Para que serve

Cada frontend emissor autentica no ingestor via header
`X-Api-Key`. Essa tela cria, lista e **revoga** as chaves — uma por
app. O backend guarda o hash para autenticação e também uma cópia cifrada
para consulta administrativa posterior. Cada nova chave de um app revoga a
anterior na mesma transação e mantém ambas no histórico com datas de criação
e revogação.

## Onde fica

- Rota: `/admin/keys`
- Arquivo: `dashboard/src/app/admin/keys/page.tsx`
- Endpoints:
  - `GET /v1/admin/api-keys` (retorna chaves descriptografadas para admins)
  - `POST /v1/admin/api-keys` (gera e armazena uma chave cifrada)
  - `DELETE /v1/admin/api-keys/{id}` (revoga)
- Papel mínimo: **admin**

## Como usar

1. Abra **Chaves de API**. A listagem traz app, descrição, chave, status,
   data de criação, data de revogação e ações.
2. Clique **Nova chave**. Selecione o **App emissor** e opcionalmente
   uma **Descrição** ("produção web", "ambiente stg de QA").
3. Use **ver** ou **copiar** para consultar a chave ativa ou qualquer item
   do histórico. Chaves legadas sem valor cifrado precisam ser rotacionadas.
4. Clique **gerar nova** na linha do app para rotacionar: a chave ativa
   anterior será revogada automaticamente.
5. A data de revogação fica registrada no histórico. A revogação invalida
   o cache local imediatamente.

## O que você vê

- Cada linha permite revelar/copiar a chave, gerar uma nova ou revogar
  a ativa.
- Tabela: **App**, **Descrição**, **Chave**, **Status**, **Criada em**,
  **Revogada em** e **Ações**.
- Estado vazio: "Nenhuma chave ainda — comece pelo botão 'Nova chave'."

## Como demonstrar

> "Posso consultar a chave quando preciso. Ao gerar outra, a chave ativa
> anterior é revogada e fica no histórico com sua data de revogação."

Roteiro de 60s:
1. Revele e copie a chave atual na listagem.
2. Clique **gerar nova** para `portal-cliente`; a chave anterior será
   revogada automaticamente.
3. Consulte as datas de criação e revogação no histórico.
4. Aponte para `/admin/audit-log` — os eventos `apikey.create` e
   `apikey.revoke` já estão lá com quem executou.

## Papéis (RBAC)

- Toda a tela é **admin only**. Editor/viewer não veem o link.
- Admin de Company A só vê chaves dos apps de A —
  `tenantScope` no back garante isolamento.
- `is_super` vê chaves de todas as empresas.

## Dependências

- Precisa de **app cadastrado** em [`/admin/apps`](admin-apps.md) — o
  dropdown "App emissor" só lista apps existentes.
- O ingestor (`/v1/events`) usa o hash para autenticar. A API invalida
  seu cache in-memory quando a chave ativa é substituída ou revogada.

## Perguntas frequentes

- **"Chaves antigas aparecem completas?"**
  Só as geradas após a implantação do armazenamento cifrado. Chaves mais
  antigas têm apenas hash e prefixo e precisam ser rotacionadas.
- **"Posso ter várias chaves ativas para o mesmo app?"**  
  Não. A rotação revoga a chave anterior na mesma transação.
- **"O prefixo `ndk_abc12…` identifica a chave completa?"**  
  Não — é apenas para achar a linha certa quando você tem várias.
  O prefixo só identifica registros legados que não têm valor cifrado.
- **"Revogação é imediata?"**  
  Sim, na prática. O DELETE marca `revoked_at`, invalida o cache in-memory
  da API e desliga a chave em segundos.
- As chaves são cifradas com `NDOVU_API_KEY_ENC_KEY` (AES-256-GCM). Mantenha
  essa chave estável e com backup seguro; perdê-la impede revelar os valores,
  mas os hashes continuam permitindo validar a ingestão.

## Referências

- Página: `dashboard/src/app/admin/keys/page.tsx`
- API client: `dashboard/src/lib/api.ts` → `listKeys`, `createKey`,
  `revokeKey`
- Handler backend: `backend/internal/adapter/httpapi/handlers.go` —
  handlers de `/v1/admin/api-keys` e ingestor
- Docs relacionadas: [admin-apps.md](admin-apps.md),
  [admin-audit-log.md](admin-audit-log.md)
