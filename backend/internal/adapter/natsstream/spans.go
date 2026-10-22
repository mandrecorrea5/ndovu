package natsstream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

const (
	// spanPrefix prefixa os subjects dos spans OTLP: ndovu.spans.<app>.
	spanPrefix = "ndovu.spans."
	// spanDLQPrefix fica dentro de dlqPrefix (mesmo filtro e retenção); o
	// token a mais não colide com a DLQ de eventos, que é ndovu.dlq.<app>.
	spanDLQPrefix = dlqPrefix + "spans."
	// SpanConsumerName é o consumidor durável dos spans — separado do de
	// eventos para um backlog de spans nunca atrasar o rastro do frontend.
	SpanConsumerName = "ndovu-span-writer"
	// maxSpanPayload fica abaixo do max_payload default do NATS (1 MB), com
	// folga para headers.
	maxSpanPayload = 900 << 10
)

type spanEnvelope struct {
	V     int              `json:"v"`
	Batch domain.SpanBatch `json:"batch"`
}

// PublishSpans publica o lote em ndovu.spans.<app>, partindo em mensagens
// menores se o JSON passar do limite do NATS. Cada pedaço tem Msg-Id pelo
// hash do conteúdo: o reenvio do exporter (mesmo corpo) é deduplicado na
// janela do stream.
//
// Sem span próprio de propósito: o Ndovu pode exportar os próprios traces
// para o próprio receptor OTLP, e rastrear esse caminho realimentaria o loop.
func (p *Publisher) PublishSpans(ctx context.Context, batch domain.SpanBatch) error {
	return p.publishSpanChunks(ctx, spanPrefix+subjectToken(batch.App), batch, "")
}

// DeadLetterSpans manda um lote recusado pelo armazém para ndovu.dlq.spans.<app>.
func (p *Publisher) DeadLetterSpans(ctx context.Context, batch domain.SpanBatch, reason string) error {
	return p.publishSpanChunks(ctx, spanDLQPrefix+subjectToken(batch.App), batch, reason)
}

func (p *Publisher) publishSpanChunks(ctx context.Context, subject string, batch domain.SpanBatch, dlqReason string) error {
	if len(batch.Spans) == 0 {
		return nil
	}
	payload, err := json.Marshal(spanEnvelope{V: 1, Batch: batch})
	if err != nil {
		return fmt.Errorf("serializando spans: %w", err)
	}
	if len(payload) > maxSpanPayload {
		if len(batch.Spans) == 1 {
			// O orçamento por span do adapter OTLP impede isso; se acontecer,
			// é bug — melhor falhar alto do que travar o stream.
			return fmt.Errorf("span único de %d bytes excede o limite do stream", len(payload))
		}
		half := len(batch.Spans) / 2
		first := domain.SpanBatch{App: batch.App, Spans: batch.Spans[:half]}
		second := domain.SpanBatch{App: batch.App, Spans: batch.Spans[half:]}
		if err := p.publishSpanChunks(ctx, subject, first, dlqReason); err != nil {
			return err
		}
		return p.publishSpanChunks(ctx, subject, second, dlqReason)
	}

	msg := nats.NewMsg(subject)
	msg.Data = payload
	var opts []jetstream.PublishOpt
	if dlqReason != "" {
		msg.Header.Set("Ndovu-Dlq-Reason", dlqReason)
	} else {
		sum := sha256.Sum256(payload)
		opts = append(opts, jetstream.WithMsgID(hex.EncodeToString(sum[:])))
	}
	if _, err := p.js.PublishMsg(ctx, msg, opts...); err != nil {
		return fmt.Errorf("publicando spans no stream: %w", err)
	}
	return nil
}

// SpanConsumer é o pull consumer durável dos spans.
type SpanConsumer struct {
	consumer  jetstream.Consumer
	batchSize int
	maxWait   time.Duration
	logger    *slog.Logger
}

// NewSpanConsumer garante o consumidor durável de spans.
func NewSpanConsumer(ctx context.Context, js jetstream.JetStream, batchSize int, maxWait time.Duration, logger *slog.Logger) (*SpanConsumer, error) {
	cons, err := js.CreateOrUpdateConsumer(ctx, StreamName, jetstream.ConsumerConfig{
		Durable:       SpanConsumerName,
		Description:   "Writer Ndovu → ClickHouse (spans OTLP)",
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       30 * time.Second,
		MaxDeliver:    -1,
		FilterSubject: spanPrefix + ">",
	})
	if err != nil {
		return nil, fmt.Errorf("criando consumidor %s: %w", SpanConsumerName, err)
	}
	return &SpanConsumer{consumer: cons, batchSize: batchSize, maxWait: maxWait, logger: logger}, nil
}

// Run consome até o contexto encerrar, com a mesma semântica do Consumer de
// eventos: ack só depois do handler, nak (reentrega) em erro.
func (c *SpanConsumer) Run(ctx context.Context, handle func(ctx context.Context, batches []domain.SpanBatch) error) error {
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		msgs, err := c.consumer.Fetch(c.batchSize, jetstream.FetchMaxWait(c.maxWait))
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
			}
			continue
		}

		var pending []jetstream.Msg
		var batches []domain.SpanBatch
		for msg := range msgs.Messages() {
			var env spanEnvelope
			if err := json.Unmarshal(msg.Data(), &env); err != nil {
				c.logger.Error("mensagem de spans corrompida no stream descartada", "err", err)
				_ = msg.Ack()
				continue
			}
			pending = append(pending, msg)
			batches = append(batches, env.Batch)
		}
		if len(batches) == 0 {
			continue
		}

		if err := handle(ctx, batches); err != nil {
			c.logger.Error("falha ao processar spans; reentregando", "batches", len(batches), "err", err)
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
