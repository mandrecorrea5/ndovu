# Ndovu Go SDK

Cliente Go sem dependências externas para enviar eventos ao endpoint de ingestão
`POST /v1/events`.

## Instalação

```bash
go get github.com/mandrecorrea5/ndovu/sdk/go
```

## Uso

```go
package main

import (
	"context"
	"log"
	"os"
	"time"

	ndovu "github.com/mandrecorrea5/ndovu/sdk/go"
)

func main() {
	client, err := ndovu.New(ndovu.Config{
		Endpoint:      "https://api.ndovu.solutions",
		APIKey:        os.Getenv("NDOVU_API_KEY"),
		App:           "api-pedidos",
		FlushInterval: 5 * time.Second,
		RequestTimeout: 10 * time.Second,
		MaxBatch:      20,
		Release:       "1.2.3",
		OnError:       func(err error) { log.Printf("Ndovu: %v", err) },
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer client.Close(ctx)

	if err := client.Action("pedido_criado", ndovu.EventOptions{
		Feature: "pedidos",
		Metadata: map[string]any{
			"orderId": "ord-123",
		},
	}); err != nil {
		log.Fatal(err)
	}
}
```

O app deve existir no Ndovu, e seu nome precisa corresponder ao app associado
à chave. Guarde `NDOVU_API_KEY` em um secret manager ou variável de ambiente;
não a versione.

## API

- `PageView(screen, options)` grava `page_view`.
- `Action(name, options)` grava `action`.
- `Custom(name, options)` grava `custom`.
- `Error(name, ErrorOptions)` grava `error`.
- `HTTPRequest(name, HTTPRequestOptions)` grava `http_request`.
- `Flush(ctx)` envia imediatamente os eventos na fila.
- `Close(ctx)` para o flusher e tenta enviar os eventos restantes.

`EventOptions.Session` permite associar o evento a uma sessão/usuário
específico. Sem esse valor, o client usa uma sessão por processo. A redaction
substitui valores de campos com nomes como `password`, `token`, `secret`,
`authorization`, `cvv` e `card` por `***` em metadata, atributos de sessão e
corpos HTTP/erro. `RequestTimeout` limita cada envio do flusher periódico.

O flusher periódico usa `Config.OnError` (por padrão, `log.Printf`) para
reportar falhas; eventos não enviados permanecem na fila para uma tentativa
posterior. O chamador deve chamar `Close` durante o encerramento gracioso.
