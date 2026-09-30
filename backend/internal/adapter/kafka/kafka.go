// Package kafka implementa o buffer durável de ingestão sobre Apache Kafka,
// substituindo o NATS JetStream de forma plugável via `domain.EventStream`.
//
// Desenho:
//   - Tópico `ndovu.events` particionado por session_id (hash da chave →
//     partição): eventos da mesma sessão caem sempre na mesma partição, então
//     o processamento preserva a ordem por sessão e nunca se espalha entre
//     consumers do grupo.
//   - Producer idempotente (`RequiredAcks: RequireAll`): elimina duplicação
//     do lado do producer em retries. A deduplicação final por eventId fica no
//     ReplacingMergeTree do ClickHouse (at-least-once).
//   - Consumer Group `ndovu-writer`: o commit de offset acontece SÓ após o
//     insert no ClickHouse. Se o handler falha, o offset não é commitado e o
//     Kafka re-entrega a mensagem (at-least-once).
//   - Tópico `ndovu.dlq` para lotes rejeitados permanentemente pelo armazém,
//     com a razão em header, para inspeção/reprocessamento manual.
package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

const (
	// EnvelopeVersion versiona o payload transportado no tópico.
	EnvelopeVersion = 1
)

// Config carrega a configuração de conexão com o Kafka.
type Config struct {
	Brokers          []string      // ex.: broker1:9092,broker2:9092
	EventsTopic      string        // tópico principal de ingestão
	DLQTopic         string        // tópico de dead-letter
	ConsumerGroup    string        // grupo do writer
	Partitions       int           // número de partições do tópico de eventos
	ReplicationFactor int          // fator de replicação dos tópicos
	RetentionHours   time.Duration // janela de replay no tópico de eventos
	MaxPollRecords   int           // máx. mensagens por ciclo do consumer
	FetchMaxWait     time.Duration // espera máxima por batch no consumer
}

// envelope versiona o formato transportado no tópico.
type envelope struct {
	V     int                `json:"v"`
	Batch domain.IngestBatch `json:"batch"`
}

// connect abre o dialer com retry (útil na subida do compose).
func connect(brokers []string) *kafka.Dialer {
	return &kafka.Dialer{
		Timeout:   5 * time.Second,
		DualStack: true,
	}
}

// EnsureTopics cria (ou ajusta) os tópicos de eventos e DLQ se não existirem.
// Ajustes de partições/retenção em tópicos existentes exigem admin manual no
// cluster; aqui só garantimos a existência.
func EnsureTopics(ctx context.Context, cfg Config, logger *slog.Logger) error {
	conn, err := kafka.DialContext(ctx, "tcp", cfg.Brokers[0])
	if err != nil {
		return fmt.Errorf("conectando ao kafka: %w", err)
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("resolvendo controller: %w", err)
	}
	ctrlConn, err := kafka.DialContext(ctx, "tcp", controller.Host+":"+strconv.Itoa(controller.Port))
	if err != nil {
		return fmt.Errorf("conectando ao controller: %w", err)
	}
	defer ctrlConn.Close()

	for _, topic := range []struct {
		name    string
		part    int
		repl    int
	}{
		{cfg.EventsTopic, cfg.Partitions, cfg.ReplicationFactor},
		{cfg.DLQTopic, 3, cfg.ReplicationFactor},
	} {
		if err := ctrlConn.CreateTopics(kafka.TopicConfig{
			Topic:             topic.name,
			NumPartitions:     topic.part,
			ReplicationFactor: topic.repl,
		}); err != nil {
			if errors.Is(err, kafka.TopicAlreadyExists) {
				logger.Info("tópico já existe", "topic", topic.name)
				continue
			}
			// CreateTopics não altera tópicos existentes com configuração
			// diferente; se a falha for de fator de replicação, o tópico já
			// está lá e seguimos.
			if errors.Is(err, kafka.InvalidReplicationFactor) {
				logger.Warn("tópico já existe (factor diferente) — ignorado", "topic", topic.name)
				continue
			}
			return fmt.Errorf("criando tópico %s: %w", topic.name, err)
		}
		logger.Info("tópico garantido", "topic", topic.name, "partitions", topic.part)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Producer (lado da API de ingestão)
// ---------------------------------------------------------------------------

// Publisher implementa domain.EventStream e DeadLetterSink sobre Kafka.
type Publisher struct {
	writer      *kafka.Writer
	eventsTopic string
	dlqTopic    string
}

// NewPublisher cria o publisher. O hash da chave (session_id) determina a
// partição, preservando a ordem por sessão.
func NewPublisher(cfg Config) (*Publisher, error) {
	w := &kafka.Writer{
		Addr:         kafka.TCP(cfg.Brokers...),
		Topic:        cfg.EventsTopic,
		Balancer:     &kafka.Hash{}, // partição = hash(session_id)
		RequiredAcks: kafka.RequireAll,
		Async:        false,
		Compression:  kafka.Snappy,
	}
	return &Publisher{writer: w, eventsTopic: cfg.EventsTopic, dlqTopic: cfg.DLQTopic}, nil
}

// Close libera o writer.
func (p *Publisher) Close() error { return p.writer.Close() }

// Publish serializa o lote e publica no tópico de eventos com a chave =
// session_id. Producer idempotente + dedup no ClickHouse garantem at-least-once.
func (p *Publisher) Publish(ctx context.Context, batch domain.IngestBatch) error {
	payload, err := json.Marshal(envelope{V: EnvelopeVersion, Batch: batch})
	if err != nil {
		return fmt.Errorf("serializando lote: %w", err)
	}
	if err := p.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(batch.Session.SessionID),
		Value: payload,
	}); err != nil {
		return fmt.Errorf("publicando no tópico %s: %w", p.eventsTopic, err)
	}
	return nil
}

// DeadLetter publica um lote rejeitado pelo armazém no tópico de DLQ, com a
// razão em header, para inspeção e reprocessamento manual.
func (p *Publisher) DeadLetter(ctx context.Context, batch domain.IngestBatch, reason string) error {
	payload, err := json.Marshal(envelope{V: EnvelopeVersion, Batch: batch})
	if err != nil {
		return fmt.Errorf("serializando lote para DLQ: %w", err)
	}
	msg := kafka.Message{
		Key:   []byte(batch.Session.SessionID),
		Value: payload,
		Headers: []kafka.Header{
			{Key: "ndovu-dlq-reason", Value: []byte(reason)},
		},
	}
	// DLQ usa um writer dedicado apontando para o tópico de DLQ.
	dlq := &kafka.Writer{
		Addr:         p.writer.Addr,
		Topic:        p.dlqTopic,
		Balancer:     &kafka.Hash{},
		RequiredAcks: kafka.RequireAll,
	}
	defer dlq.Close()
	if err := dlq.WriteMessages(ctx, msg); err != nil {
		return fmt.Errorf("publicando na DLQ %s: %w", p.dlqTopic, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Consumer (lado do writer)
// ---------------------------------------------------------------------------

// Consumer é o consumer group durável do writer.
type Consumer struct {
	reader  *kafka.Reader
	maxPoll int
	maxWait time.Duration
	logger  *slog.Logger
}

// NewConsumer cria o consumer group e devolve pronto para Run. O commit é
// manual (CommitInterval = 0) para que o offset só avance após o insert no
// ClickHouse ter sucesso — falha = re-entrega (at-least-once).
func NewConsumer(cfg Config, logger *slog.Logger) (*Consumer, error) {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        cfg.Brokers,
		GroupID:        cfg.ConsumerGroup,
		Topic:          cfg.EventsTopic,
		MinBytes:       1,
		MaxBytes:       10e6,
		MaxWait:        cfg.FetchMaxWait,
		CommitInterval: 0, // commit manual: só após o handler persistir
		StartOffset:    kafka.FirstOffset,
	})
	return &Consumer{reader: r, maxPoll: cfg.MaxPollRecords, maxWait: cfg.FetchMaxWait, logger: logger}, nil
}

// Close encerra o reader.
func (c *Consumer) Close() error { return c.reader.Close() }

// Run consome em loop até o contexto encerrar: acumula mensagens, entrega ao
// handler e só então commita o offset. O flush acontece quando atinge maxPoll
// OU quando maxWait expira desde a última mensagem (tráfego baixo).
//
// Em falha do handler, NÃO commitamos e retornamos o erro: o processo reinicia
// e o consumer group re-entrega do último offset commitado (at-least-once).
// Isso é o comportamento idiomático do Kafka — diferente do JetStream, que
// reentrega no mesmo processo via nak. Descartar aqui e seguir seria perder
// lotes silenciosamente.
func (c *Consumer) Run(ctx context.Context, handle func(ctx context.Context, batches []domain.IngestBatch) error) error {
	var pending []kafka.Message
	var batches []domain.IngestBatch

	flush := func() error {
		if len(batches) == 0 {
			return nil
		}
		if err := handle(ctx, batches); err != nil {
			c.logger.Error("falha ao processar lotes; offset não commitado (re-entrega no restart)",
				"batches", len(batches), "err", err)
			return err
		}
		// Sucesso: commita o lote inteiro de offsets de uma vez.
		if err := c.reader.CommitMessages(ctx, pending...); err != nil {
			c.logger.Error("falha ao commitar offsets", "err", err)
			return err
		}
		pending = pending[:0]
		batches = batches[:0]
		return nil
	}

	for {
		// Se há mensagens acumuladas mas maxWait expirou desde a última,
		// processa antes de bloquear no próximo fetch (tráfego baixo).
		if len(batches) > 0 {
			select {
			case <-ctx.Done():
				return flush()
			default:
			}
		}

		ctxFetch := ctx
		var cancel context.CancelFunc
		if len(batches) > 0 {
			// Se já temos dados acumulados, não esperamos mais que maxWait por
			// novas mensagens antes de descarregar.
			ctxFetch, cancel = context.WithTimeout(ctx, c.maxWait)
		}

		msg, err := c.reader.FetchMessage(ctxFetch)
		if cancel != nil {
			cancel()
		}

		if err != nil {
			if len(batches) > 0 {
				// Timeout do fetch com dados pendentes: descarrega.
				return flush()
			}
			if ctx.Err() != nil {
				return nil
			}
			// Erro transitório (ex.: rebalanceamento): aguarda e tenta de novo.
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
			}
			continue
		}

		var env envelope
		if err := json.Unmarshal(msg.Value, &env); err != nil {
			// Mensagem corrompida nunca vai processar: commita para não travar.
			c.logger.Error("mensagem corrompida no tópico descartada", "err", err)
			if err := c.reader.CommitMessages(ctx, msg); err != nil {
				c.logger.Warn("falha ao commitar mensagem corrompida", "err", err)
			}
			continue
		}
		pending = append(pending, msg)
		batches = append(batches, env.Batch)

		if len(batches) >= c.maxPoll {
			return flush()
		}
	}
}
