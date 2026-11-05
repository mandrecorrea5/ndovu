# Seleção global de App

## Para que serve

Evita escolher o app em **cada tela**. O usuário escolhe uma vez e toda a
navegação do dashboard (visão geral, issues, traces, sessões, performance,
releases, retenção, funis e logs) passa a consultar aquele app.

Regra de ouro:

- **mais de um app acessível** → aparece o seletor global de App no topo;
- **exatamente um app** → seleção automática, seletor escondido e navegação
  normal;
- **nenhum app** → nada muda (o backend já restringe tudo pelo tenant scope).

## Como usar

1. Faça login. Se você tem acesso a mais de um app, a barra **App** aparece
   no topo da área de conteúdo.
2. Escolha um app (ou mantenha **Todos os apps** para ver o agregado do seu
   acesso).
3. Navegue normalmente: a escolha continua valendo em todas as telas e em
   reloads seguintes (fica em `localStorage`, por usuário).
4. Troque de app quando quiser — a próxima consulta já usa o novo escopo.

O seletor não aparece nas telas de **Administração** (`/admin/*`): elas
gerenciam apps, chaves e usuários de forma global.

## Detalhes de comportamento

- A lista do seletor vem de `GET /v1/meta/apps`, que aplica o mesmo
  `tenantScope` das consultas (company + grants explícitos).
- **Todos os apps** significa “todos os apps que você pode ver”, não todos os
  apps da plataforma.
- Links antigos com `?app=<nome>` (ex.: “ver eventos” de uma issue) continuam
  funcionando: o app do link é promovido a escopo global e sai da URL.
- Saved views que guardaram `app` também trocam o app global ao serem
  aplicadas.
- Se o acesso a um app for revogado, ele deixa de aparecer na lista e a
  seleção inválida cai para “Todos os apps” (ou para o único app restante).

## Referências

- Provider/estado: `dashboard/src/lib/app-scope.tsx`
- Seletor: `dashboard/src/components/AppScopeBar.tsx`
- Shell: `dashboard/src/components/AppShell.tsx`
- BFF: `dashboard/src/app/api/meta/apps/route.ts`
- API: `GET /v1/meta/apps` (handler `GetMyApps`)
