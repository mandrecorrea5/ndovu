# Changelog do SDK

Formato seguir: um bloco por versão publicada, mais recente primeiro. Escreva o
efeito para quem **integra**, não o diff — a maior parte dos consumidores chega
por cópia do arquivo (docs/INTEGRATION.md, "Opção A") e não tem um
`npm diff` para consultar.

A versão canônica de cada entry point é a constante `SDK_VERSION` no topo do
arquivo, e é ela que viaja no campo `sdkVersion` do envelope.

Nota: até aqui o backend **recebia** `sdkVersion` e descartava — o
`httpapi/dto.go:14` fazia o parse, mas o `toDomain()` não propagava e não havia
teste na fronteira. Corrigido no backend logo após esta release (pendente de
deploy): o valor passa a ser gravado em `session.attributes.sdkVersion`, coluna
`session_attrs` que já existia. Sem migration e sem release de cliente.

Como a correção é do lado do servidor, ela alcança quem **não** atualizou: um app
parado na 1.0.0 passa a aparecer como `1.0.0` no dado. Consulte com
`JSONExtractString(session_attrs, 'sdkVersion')`.

## 1.3.0 — 2026-10-17

`@ndovu/sdk` 1.3.0 · browser `1.3.0` · react-native `1.1.0-rn` ·
node `1.1.0-node` (inalterado) · go (inalterado)

### O envio de eventos não descarta mais em silêncio

`flush()` não checava a resposta. Um `401` de ingestão derrubava o lote inteiro
sem nenhum sinal — o app continuava rodando e os traces simplesmente
sumiam — e a fila só crescia em erro de rede, nunca em falha HTTP.

A decisão passou a ser por status:

| Resposta | Comportamento |
|---|---|
| 2xx | enviado, sem ruído |
| 400, 401, 403, 404, 413 | descartado + um `console.warn` com a causa provável |
| 5xx, 429 | de volta para a fila, com o mesmo `eventId` |
| erro de rede | de volta para a fila (como antes) |

**Atenção:** 4xx é agora descarte explícito. Antes o lote também se perdia, mas
uma correção tardia da chave reprocessava o que estivesse retido; agora o que
foi recusado não volta. Quem depende de reprocessar depois de corrigir a
`X-Api-Key` precisa tratar isso de outro lado.

Os 4xx escolhidos são erro de configuração ou de contrato — chave inválida,
`envelope.app` diferente do app da chave, payload fora do contrato — onde
reenviar não conserta nada.

### Fila de reenvio tem teto de 400 eventos

Consequência da mudança acima: com 5xx voltando para a fila, uma API
indisponível fazia a fila crescer a cada `flushIntervalMs` sem limite, até
derrubar a aba. Acima do teto sai o evento mais antigo, e um `console.warn`
informa quantos foram descartados.

O `eventId` é gerado uma vez por evento e preservado no reenvio, então o
servidor deduplica o que chegar repetido.

### Reenvio não estoura mais a pilha no React Native

`queue.unshift(...events)` com array grande estoura a pilha de chamada. O
reenvio agora concatena. Improvável enquanto só a rede re-entrava na fila;
comum com 5xx entrando.

### Aviso sai uma vez, e por `console.warn`

Sem o guard, uma chave revogada produziria um aviso a cada `flushIntervalMs`
pela vida inteira da aba. E é `console.warn`, não `console.error`: o próprio SDK
intercepta `console.error` e o converte em evento de erro, o que recursaria no
caminho de envio.

### Snapshot recusa agora é visível

`sendSnapshot()` tinha `try/catch` vazio e não checava a resposta — a tela não
era salva e não havia nenhum indício. Passa a avisar no console quando a API
recusa.

Para snapshot de tela grande, acompanha mudança no servidor:
`NDOVU_SNAPSHOT_MAX_BODY_BYTES` (24 MB) no backend. **Contra um backend antigo
o aviso continua aparecendo** — o teto de 1 MB da ingestão corta o DOM inteiro
antes de qualquer outra coisa.

### Documentação

- `NdovuAutoRouter`: `screenName` passa a ser tratado como obrigatório em rota
  dinâmica. Sem ele, o pathname cru vira nome de tela, e `pageView` compõe o
  nome do evento como `tela_${screen}` — `/atleta/123` e `/atleta/456` viram
  duas telas e dois nomes de evento no mesmo group-by. Continua opt-in no
  código; documentado com exemplo de mapeamento para o template da rota.
- Exemplos de import dos SDKs Next e React Native apontavam para
  `@your-org/ndovu-*`, pacotes que não existem. Agora usam caminho relativo,
  como já fazia o SDK Node.

### Como verificar

```bash
npm run check   # sintaxe dos 4 entry points + 18 verificações de comportamento do flush
```

## Versões anteriores

`1.2.0` (browser), `1.1.0-node` e `1.0.0-rn` chegaram juntas no import inicial
deste repositório, sem histórico individual commit a commit. Não há como
reconstruir o que cada uma introduziu sem inventar, então este arquivo começa na
1.3.0 — que é também a primeira versão com tag (`v1.3.0`) e release publicada.
