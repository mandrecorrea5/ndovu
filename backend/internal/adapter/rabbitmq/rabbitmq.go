// Package rabbitmq implementa o buffer durável de ingestão sobre RabbitMQ,
// substituindo o NATS JetStream e o Kafka de forma plugável via
// `domain.EventStream`.
//
// Desenho:
//   - Fila durável `ndovu.events` com entrega persistente (DeliveryMode
//     Persistent): mensagens sobrevivem a restart do broker e do consumer.
//   - Consumer com ack manual: `Ack` só após o insert no ClickHouse; falha =>
//     `Nack(requeue=true)` que reentrega a mensagem (at-least-once) — semântica
//     idêntica ao JetStream (nak), diferente do Kafka que reentrega no restart.
//   - Fila `ndovu.dlq` para lotes rejeitados permanentemente pelo armazém.
//
// NOTA: o RabbitMQ não tem particionamento nativo (como a partição por
// session_id do Kafka). A ordem por sessão não é garantida globalmente; para o
// caso de uso (at-least-once + dedup por eventId no ReplacingMergeTree) isso é
// aceitável. Se a ordem por sessão for um requisito rígido, prefira o Kafka.
package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

const EnvelopeVersion = 1

// Config carrega a configuração de conexão com o RabbitMQ.
type Config struct {
	URL          string        // amqp://user:pass@host:5672/vhost
	EventsQueue  string        // fila principal de ingestão
	DLQQueue     string        // fila de dead-letter
	ConsumerName string        // consumer tag do writer
	MaxPoll      int           // mensagens acumuladas por ciclo
	MaxWait      time.Duration // espera máxima por lote (tráfego baixo)
}

// envelope versiona o formato transportado na fila.
type envelope struct {
	V     int                `json:"v"`
	Batch domain.IngestBatch `json:"batch"`
}

// Connect abre a conexão + canal com retry (útil na subida do compose).
func Connect(url string) (*amqp.Connection, *amqp.Channel, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, nil, fmt.Errorf("conectando ao rabbitmq: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("abrindo canal: %w", err)
	}
	return conn, ch, nil
}

// EnsureQueues cria (ou garante) as filas de eventos e DLQ. Declarar uma fila
// já existente é idempotente no RabbitMQ.
func EnsureQueues(ch *amqp.Channel, cfg Config) error {
	for _, q := range []struct {
		name string
		args amqp.Table
	}{
		{cfg.EventsQueue, nil},
		{cfg.DLQQueue, nil},
	} {
		_, err := ch.QueueDeclare(q.name, true, false, false, false, q.args)
		if err != nil {
			return fmt.Errorf("declarando fila %s: %w", q.name, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Publisher (lado da API de ingestão)
// ---------------------------------------------------------------------------

// Publisher implementa domain.EventStream e DeadLetterSink sobre RabbitMQ.
type Publisher struct {
	ch          *amqp.Channel
	eventsQueue string
	dlqQueue    string
}

// NewPublisher cria o publisher sobre um canal já garantido.
func NewPublisher(ch *amqp.Channel, cfg Config) *Publisher {
	return &Publisher{ch: ch, eventsQueue: cfg.EventsQueue, dlqQueue: cfg.DLQQueue}
}

// Publish serializa o lote e publica na fila de eventos com entrega persistente.
func (p *Publisher) Publish(ctx context.Context, batch domain.IngestBatch) error {
	payload, err := json.Marshal(envelope{V: EnvelopeVersion, Batch: batch})
	if err != nil {
		return fmt.Errorf("serializando lote: %w", err)
	}
	err = p.ch.PublishWithContext(ctx, "", p.eventsQueue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         payload,
	})
	if err != nil {
		return fmt.Errorf("publicando na fila %s: %w", p.eventsQueue, err)
	}
	return nil
}

// DeadLetter publica um lote rejeitado pelo armazém na fila de DLQ.
func (p *Publisher) DeadLetter(ctx context.Context, batch domain.IngestBatch, reason string) error {
	payload, err := json.Marshal(envelope{V: EnvelopeVersion, Batch: batch})
	if err != nil {
		return fmt.Errorf("serializando lote para DLQ: %w", err)
	}
	err = p.ch.PublishWithContext(ctx, "", p.dlqQueue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         payload,
		Headers: amqp.Table{
			"ndovu-dlq-reason": reason,
		},
	})
	if err != nil {
		return fmt.Errorf("publicando na DLQ %s: %w", p.dlqQueue, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Consumer (lado do writer)
// ---------------------------------------------------------------------------

// Consumer é o consumidor durável do writer.
type Consumer struct {
	ch      *amqp.Channel
	queue   string
	tag     string
	maxPoll int
	maxWait time.Duration
	logger  *slog.Logger
}

// NewConsumer guarda a configuração de consumo; o Consume em si é registrado
// no Run (com a tag única por instância).
func NewConsumer(ch *amqp.Channel, cfg Config, logger *slog.Logger) (*Consumer, error) {
	return &Consumer{ch: ch, queue: cfg.EventsQueue, tag: cfg.ConsumerName, maxPoll: cfg.MaxPoll, maxWait: cfg.MaxWait, logger: logger}, nil
}

// Close cancela o consumo.
func (c *Consumer) Close() error {
	return c.ch.Cancel(c.tag, false)
}

// Run consome em loop até o contexto encerrar. Acumula até maxPoll mensagens,
// entrega ao handler e dá Ack em todas no sucesso. Em falha, dá Nack(requeue)
// → a mensagem volta para a fila (reentrega no mesmo processo, at-least-once).
func (c *Consumer) Run(ctx context.Context, handle func(ctx context.Context, batches []domain.IngestBatch) error) error {
	deliveries, err := c.ch.Consume(c.queue, c.tag, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("registrando consumo: %w", err)
	}

	var pending []amqp.Delivery
	var batches []domain.IngestBatch

	flush := func() error {
		if len(batches) == 0 {
			return nil
		}
		if err := handle(ctx, batches); err != nil {
			// Requeue: a mensagem volta para a fila e é reentregue (at-least-once,
			// semântica igual ao JetStream nak). Continua o loop.
			c.logger.Error("falha ao processar lotes; requeue", "batches", len(batches), "err", err)
			for _, d := range pending {
				_ = d.Nack(false /* multiple */, true /* requeue */)
			}
			pending = pending[:0]
			batches = batches[:0]
			// Backoff leve antes de repoll para não martelar a fila.
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(2 * time.Second):
			}
			return nil
		}
		for _, d := range pending {
			if err := d.Ack(false /* multiple */); err != nil {
				c.logger.Error("falha ao fazer ack", "err", err)
			}
		}
		pending = pending[:0]
		batches = batches[:0]
		return nil
	}

	for {
		if len(batches) > 0 {
			select {
			case <-ctx.Done():
				_ = flush()
				return nil
			default:
			}
		}

		// Lê uma mensagem com timeout para permitir flush por tempo (tráfego baixo).
		timeout := time.After(c.maxWait)
		select {
		case <-ctx.Done():
			return nil
		case <-timeout:
			_ = flush()
		case d, ok := <-deliveries:
			if !ok {
				return nil
			}
			var env envelope
			if err := json.Unmarshal(d.Body, &env); err != nil {
				// Mensagem corrompida nunca vai processar: ack para não travar.
				c.logger.Error("mensagem corrompida na fila descartada", "err", err)
				_ = d.Ack(false)
				continue
			}
			pending = append(pending, d)
			batches = append(batches, env.Batch)
			if len(batches) >= c.maxPoll {
				_ = flush()
			}
		}
	}
}
