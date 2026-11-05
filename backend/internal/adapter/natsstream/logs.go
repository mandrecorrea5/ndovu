package natsstream

// Transporte dos logs OTLP no JetStream. Mesmo desenho dos spans: subject por
// app, split abaixo do max_payload e Msg-Id pelo hash do conteúdo.

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
	logPrefix    = "ndovu.logs."
	logDLQPrefix = dlqPrefix + "logs."
	// LogConsumerName é separado do de eventos e do de spans para um backlog
	// de logs nunca atrasar traces nem page views.
	LogConsumerName = "ndovu-log-writer"
	maxLogPayload   = 900 << 10
)

type logEnvelope struct {
	V     int             `json:"v"`
	Batch domain.LogBatch `json:"batch"`
}

// PublishLogs publica o lote em ndovu.logs.<app>, partindo se necessário.
func (p *Publisher) PublishLogs(ctx context.Context, batch domain.LogBatch) error {
	return p.publishLogChunks(ctx, logPrefix+subjectToken(batch.App), batch, "")
}

// DeadLetterLogs manda um lote recusado pelo armazém para ndovu.dlq.logs.<app>.
func (p *Publisher) DeadLetterLogs(ctx context.Context, batch domain.LogBatch, reason string) error {
	return p.publishLogChunks(ctx, logDLQPrefix+subjectToken(batch.App), batch, reason)
}

func (p *Publisher) publishLogChunks(ctx context.Context, subject string, batch domain.LogBatch, dlqReason string) error {
	if len(batch.Logs) == 0 {
		return nil
	}
	payload, err := json.Marshal(logEnvelope{V: 1, Batch: batch})
	if err != nil {
		return fmt.Errorf("serializando logs: %w", err)
	}
	if len(payload) > maxLogPayload {
		if len(batch.Logs) == 1 {
			return fmt.Errorf("log único de %d bytes excede o limite do stream", len(payload))
		}
		half := len(batch.Logs) / 2
		first := domain.LogBatch{App: batch.App, Logs: batch.Logs[:half]}
		second := domain.LogBatch{App: batch.App, Logs: batch.Logs[half:]}
		if err := p.publishLogChunks(ctx, subject, first, dlqReason); err != nil {
			return err
		}
		return p.publishLogChunks(ctx, subject, second, dlqReason)
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
		return fmt.Errorf("publicando logs no stream: %w", err)
	}
	return nil
}

// LogConsumer é o pull consumer durável dos logs.
type LogConsumer struct {
	consumer  jetstream.Consumer
	batchSize int
	maxWait   time.Duration
	logger    *slog.Logger
}

// NewLogConsumer garante o consumidor durável de logs.
func NewLogConsumer(ctx context.Context, js jetstream.JetStream, batchSize int, maxWait time.Duration, logger *slog.Logger) (*LogConsumer, error) {
	cons, err := js.CreateOrUpdateConsumer(ctx, StreamName, jetstream.ConsumerConfig{
		Durable:       LogConsumerName,
		Description:   "Writer Ndovu → ClickHouse (logs OTLP)",
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       30 * time.Second,
		MaxDeliver:    -1,
		FilterSubject: logPrefix + ">",
	})
	if err != nil {
		return nil, fmt.Errorf("criando consumidor %s: %w", LogConsumerName, err)
	}
	return &LogConsumer{consumer: cons, batchSize: batchSize, maxWait: maxWait, logger: logger}, nil
}

// Run consome até o contexto encerrar, com ack só depois do handler.
func (c *LogConsumer) Run(ctx context.Context, handle func(ctx context.Context, batches []domain.LogBatch) error) error {
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
		var batches []domain.LogBatch
		for msg := range msgs.Messages() {
			var env logEnvelope
			if err := json.Unmarshal(msg.Data(), &env); err != nil {
				c.logger.Error("mensagem de logs corrompida no stream descartada", "err", err)
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
			c.logger.Error("falha ao processar logs; reentregando", "batches", len(batches), "err", err)
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
