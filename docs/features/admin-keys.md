# Chaves de API (`/admin/keys`)

## Para que serve

Cada frontend emissor autentica no ingestor via header
`X-Api-Key`. Essa tela cria, lista e **revoga** as chaves — uma por
app. O backend guarda o hash para autenticação e também uma cópia cifrada
para que administradores autorizados possam consultar o valor depois. Cada
nova chave de um app revoga a anterior na mesma transação e mantém ambas no
histórico, com datas de criação e revogação.

## Onde fica

- Rota: `/admin/keys`
- Arquivo: `dashboard/src/app/admin/keys/page.tsx`
- Endpoints:
  - `GET /v1/admin/api-keys` (retorna as chaves descriptografadas para admins)
  - `POST /v1/admin/api-keys` (gera e armazena uma chave cifrada)
  - `DELETE /v1/admin/api-keys/{id}` (revoga)
- Papel mínimo: **admin**

## Como usar

1. Abra **Chaves de API**. A listagem traz app, descrição, prefixo,
   status, data de criação e data de revogação.
2. Clique **Nova chave**. Selecione o **App emissor** e opcionalmente
   uma **Descrição** ("produção web", "ambiente stg de QA").
3. Use **ver** ou **copiar** para consultar a chave ativa ou qualquer item
   do histórico. Chaves anteriores ao suporte a armazenamento cifrado
   mostram somente o prefixo e não podem ser recuperadas.
4. Clique **gerar nova** na linha do app (ou **Nova chave**) para rotacionar.
   A chave anterior é revogada automaticamente; atualize a integração com
   a nova chave.
5. A data de revogação fica registrada no histórico. A revogação invalida
   imediatamente o cache local da API.

## O que você vê

- Cada linha permite revelar/copiar a chave, rotacionar ou revogar.
- Tabela: **App**, **Descrição**, **Chave**, **Status**, **Criada em**,
  **Revogada em** e **Ações**.
- Estado vazio: "Nenhuma chave ainda — comece pelo botão 'Nova chave'."

## Como demonstrar

> "A chave pode ser consultada novamente. Quando gero outra, a antiga é
> revogada e fica no histórico com a data; atualizo a integração para usar
> a nova."

Roteiro de 60s:
1. Revele ou copie a chave ativa na listagem.
2. Clique **gerar nova** para `portal-cliente`; a anterior passa
   automaticamente a revogada.
3. Confirme as datas de criação e revogação no histórico.
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
- O ingestor (`/v1/events`) usa o hash para autenticar. Cache in-memory
  no writer é invalidado no DELETE — se você roda múltiplos writers,
  todos escutam o mesmo evento de invalidação.

## Perguntas frequentes

- **"Chaves antigas aparecem completas?"**
  Somente as geradas após a implantação do armazenamento cifrado. As
  anteriores têm apenas hash e prefixo, então precisam ser rotacionadas.
- **"Posso ter várias chaves ativas para o mesmo app?"**  
  Não. A rotação revoga a chave ativa anterior na mesma transação.
- **"O prefixo `ndk_abc12…` identifica a chave completa?"**  
  Não — é apenas um identificador parcial para histórico legado sem valor
  cifrado.
- **"Revogação é imediata?"**  
  Sim, na prática. O DELETE marca `revoked_at`, invalida o cache
  in-memory dos writers e desliga a chave em segundos.
- As chaves são cifradas com `NDOVU_API_KEY_ENC_KEY` (AES-256-GCM). A chave
  precisa ser estável e ter pelo menos 32 caracteres; perdê-la impede revelar
  os valores cifrados, mas os hashes continuam validando a ingestão.

## Referências

- Página: `dashboard/src/app/admin/keys/page.tsx`
- API client: `dashboard/src/lib/api.ts` → `listKeys`, `createKey`,
  `revokeKey`
- Handler backend: `backend/internal/adapter/httpapi/handlers.go` —
  handlers de `/v1/admin/api-keys` e ingestor
- Docs relacionadas: [admin-apps.md](admin-apps.md),
  [admin-audit-log.md](admin-audit-log.md)
