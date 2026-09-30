// Package natsstream implementa o port EventStream sobre NATS JetStream.
//
// Desenho:
//   - Stream "NDOVU" (file storage, retenção por idade) recebe lotes em
//     ndovu.events.<app>. A retenção dá janela de replay/reprocessamento.
//   - Publish usa Nats-Msg-Id com hash do payload: o JetStream descarta
//     re-publicações idênticas dentro da janela de dedup (retry da API/SDK).
//   - O writer consome com um pull consumer durável em lotes; ack só após o
//     insert no ClickHouse (at-least-once — o eventId deduplica no armazém).
package natsstream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

const (
	// StreamName é o nome do stream JetStream.
	StreamName = "NDOVU"
	// subjectPrefix prefixa os subjects de eventos.
	subjectPrefix = "ndovu.events."
	// dlqPrefix prefixa os subjects de dead-letter (lotes rejeitados pelo armazém).
	dlqPrefix = "ndovu.dlq."
	// ConsumerName é o consumidor durável do writer.
	ConsumerName = "ndovu-writer"
)

// envelope versiona o formato transportado no stream, permitindo evoluir o
// payload interno sem quebrar mensagens em trânsito.
type envelope struct {
	V     int                `json:"v"`
	Batch domain.IngestBatch `json:"batch"`
}

// Connect abre a conexão NATS com retry (útil na subida do compose).
func Connect(url string) (*nats.Conn, error) {
	return nats.Connect(url,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.RetryOnFailedConnect(true),
		nats.Timeout(5*time.Second),
	)
}

// EnsureStream cria (ou atualiza) o stream com retenção por idade.
func EnsureStream(ctx context.Context, nc *nats.Conn, maxAge time.Duration) (jetstream.JetStream, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("inicializando jetstream: %w", err)
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        StreamName,
		Description: "Lotes de traces dos frontends (contrato v1)",
		Subjects:    []string{subjectPrefix + ">", dlqPrefix + ">"},
		Storage:     jetstream.FileStorage,
		Retention:   jetstream.LimitsPolicy,
		MaxAge:      maxAge,
		Duplicates:  2 * time.Minute,
	})
	if err != nil {
		return nil, fmt.Errorf("criando stream %s: %w", StreamName, err)
	}
	return js, nil
}

// ---------------------------------------------------------------------------
// Publisher (lado da API de ingestão)
// ---------------------------------------------------------------------------

// Publisher implementa domain.EventStream.
type Publisher struct {
	js jetstream.JetStream
}

// NewPublisher cria o publisher sobre um JetStream já garantido.
func NewPublisher(js jetstream.JetStream) *Publisher {
	return &Publisher{js: js}
}

// Publish serializa o lote e publica com Msg-Id determinístico (dedup de retry).
func (p *Publisher) Publish(ctx context.Context, batch domain.IngestBatch) error {
	payload, err := json.Marshal(envelope{V: 1, Batch: batch})
	if err != nil {
		return fmt.Errorf("serializando lote: %w", err)
	}
	sum := sha256.Sum256(payload)
	_, err = p.js.Publish(ctx, subjectFor(batch.Session.App), payload,
		jetstream.WithMsgID(hex.EncodeToString(sum[:])),
	)
	if err != nil {
		return fmt.Errorf("publicando no stream: %w", err)
	}
	return nil
}

// DeadLetter publica um lote rejeitado pelo armazém em ndovu.dlq.<app>.
// A DLQ fica no mesmo stream (mesma retenção/replay), fora do filtro do writer —
// dá para inspecionar e reprocessar depois de corrigir a causa.
func (p *Publisher) DeadLetter(ctx context.Context, batch domain.IngestBatch, reason string) error {
	payload, err := json.Marshal(envelope{V: 1, Batch: batch})
	if err != nil {
		return fmt.Errorf("serializando lote para DLQ: %w", err)
	}
	msg := nats.NewMsg(dlqPrefix + subjectToken(batch.Session.App))
	msg.Data = payload
	msg.Header.Set("Ndovu-Dlq-Reason", reason)
	if _, err := p.js.PublishMsg(ctx, msg); err != nil {
		return fmt.Errorf("publicando na DLQ: %w", err)
	}
	return nil
}

// subjectFor sanitiza o nome do app para virar token de subject NATS.
func subjectFor(app string) string {
	return subjectPrefix + subjectToken(app)
}

func subjectToken(app string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, app)
	if clean == "" {
		clean = "unknown"
	}
	return clean
}

// ---------------------------------------------------------------------------
// Consumer (lado do writer)
// ---------------------------------------------------------------------------

// Consumer é o pull consumer durável do writer.
type Consumer struct {
	consumer  jetstream.Consumer
	batchSize int
	maxWait   time.Duration
	logger    *slog.Logger
}

// NewConsumer garante o consumidor durável e o devolve pronto para Run.
func NewConsumer(ctx context.Context, js jetstream.JetStream, batchSize int, maxWait time.Duration, logger *slog.Logger) (*Consumer, error) {
	cons, err := js.CreateOrUpdateConsumer(ctx, StreamName, jetstream.ConsumerConfig{
		Durable:       ConsumerName,
		Description:   "Writer Ndovu → ClickHouse",
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       30 * time.Second,
		MaxDeliver:    -1, // reentrega até conseguir — o armazém deduplica
		FilterSubject: subjectPrefix + ">",
	})
	if err != nil {
		return nil, fmt.Errorf("criando consumidor %s: %w", ConsumerName, err)
	}
	return &Consumer{consumer: cons, batchSize: batchSize, maxWait: maxWait, logger: logger}, nil
}

// Run consome em loop até o contexto encerrar: busca até batchSize mensagens
// (esperando no máximo maxWait), entrega ao handler e dá ack em caso de sucesso.
func (c *Consumer) Run(ctx context.Context, handle func(ctx context.Context, batches []domain.IngestBatch) error) error {
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		msgs, err := c.consumer.Fetch(c.batchSize, jetstream.FetchMaxWait(c.maxWait))
		if err != nil {
			// Timeout de fetch vazio é fluxo normal; outros erros aguardam e seguem.
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
			}
			continue
		}

		var pending []jetstream.Msg
		var batches []domain.IngestBatch
		for msg := range msgs.Messages() {
			var env envelope
			if err := json.Unmarshal(msg.Data(), &env); err != nil {
				// Mensagem corrompida nunca vai processar: ack para não travar a fila.
				c.logger.Error("mensagem corrompida no stream descartada", "err", err)
				_ = msg.Ack()
				continue
			}
			pending = append(pending, msg)
			batches = append(batches, env.Batch)
		}
		if msgs.Error() != nil && len(batches) == 0 {
			continue
		}
		if len(batches) == 0 {
			continue
		}

		if err := handle(ctx, batches); err != nil {
			// Falha transitória de persistência: nak para reentrega (at-least-once).
			c.logger.Error("falha ao processar lotes; reentregando", "batches", len(batches), "err", err)
			for _, msg := range pending {
				_ = msg.Nak()
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(2 * time.Second):
			}
			continue
		}
		for _, msg := range pending {
			_ = msg.Ack()
		}
	}
}
