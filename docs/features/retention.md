# Retenção (`/retention`)

## Para que serve

Retenção é o único KPI que separa produto **bom** de produto **usado**.
A tela monta cohorts de usuários pela data do primeiro acesso e mostra
quantos voltaram nos dias seguintes (D1, D7, D14, D30). Ler o gráfico
é ler a sua história: se as linhas mais recentes retêm mais que as
antigas, o produto está melhorando; se caem, algo quebrou.

## Onde fica

- Rota: `/retention`
- Arquivo: `dashboard/src/app/retention/page.tsx`
- Endpoint: `GET /v1/stats/retention?from=&to=&app=&cohortBy=`
- Papel mínimo: qualquer papel autenticado

## Como usar

1. Escolha **App** e **Período**: **Últimos 30 / 60 / 90 dias**.
2. Ajuste **Coorte**: **semanal** (default), **diário** ou **mensal**
   — quanto mais granular, mais linhas.
3. A tabela vira um heatmap: cada linha é uma coorte com data de
   entrada, **Novos users** absolutos, e uma célula por offset (D1,
   D7, D14, D30) com % que retornou.
4. Passe o mouse sobre uma célula — o tooltip mostra "N de M usuários
   voltaram".
5. Clique **Atualizar** quando precisar recalcular sem trocar filtro.

## O que você vê

- **Título** "Retenção" + subtítulo "Coortes de usuários por período
  de primeiro acesso × % que retornaram em D1/D7/D14/D30".
- **Heatmap** com escala de cor em `--series-1` (accent do tema):
  quanto mais alta a %, mais forte a cor. Contraste do texto se
  ajusta automaticamente.
- Linhas com `newUsers = 0` mostram "—" nas células.
- Vazio: "Sem coortes na janela. Aumente o período ou verifique se o
  SDK está enviando userId."

## Como demonstrar

> "É o KPI que os investidores olham antes do MRR. Uma coorte forte
> significa que quem entra, fica. Aqui você vê semana a semana — se a
> linha da semana passada retém 30% no D7 e a de 3 meses atrás retinha
> 18%, você tem prova de melhoria."

Roteiro de 60s:
1. Abra `/retention` — mostre o heatmap ligado.
2. Aponte a diagonal (as células D1 vs D30) — leitura de esquerda pra
   direita conta a história de decaimento.
3. Compare uma coorte recente com uma antiga.
4. Mude **Coorte** de semanal para mensal — mostra elasticidade.
5. Filtre por **App** para ver diferença entre portais.

## Papéis (RBAC)

Todos os papéis autenticados enxergam a tela. Tenant scope aplica-se
aos apps.

## Dependências

- **`userId` estável obrigatório**. Sem userId (sessão anônima), o
  usuário conta como "novo" toda vez — a métrica vira ruído. Configure
  no SDK: `ndovu.identify(userId)` logo após o login.
- Retenção é calculada em ClickHouse por `argMin(fingerprint_at, ts)`
  para achar o primeiro acesso e comparar aparições subsequentes.

## Perguntas frequentes

- **"Por que não tem D60/D90 na tabela?"**
  Offsets default: 1, 7, 14, 30. Se a coorte é jovem (ex.: entrou há
  5 dias), D30 aparece vazio ("—") — ainda não passou tempo suficiente.
- **"A % é sobre eventos ou sobre usuários únicos?"**
  Sobre usuários únicos. `newUsers` da coorte no denominador,
  `distinct userId` que apareceu no dia offset no numerador.
- **"Consigo segmentar por segmento (país, plano)?"**
  Ainda não — só por app. Segmentação por atributo entra quando o
  esquema de eventos ganhar dimensões customizadas.

## Referências

- Página: `dashboard/src/app/retention/page.tsx`
- Handler: `backend/internal/adapter/httpapi/handlers.go` →
  `GetRetention`
- Query ClickHouse: `backend/internal/adapter/clickhouse/queries.go`
- Docs relacionadas: [funnels.md](funnels.md),
  [visao-geral.md](visao-geral.md).
