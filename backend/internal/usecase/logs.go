package usecase

// Ingestão e persistência de logs OTLP (Fase 2). Mesmo desenho dos spans,
// sem sampling/cota nesta primeira versão: o volume de log é controlado pelo
// cliente e pela retenção, e uma cota malfeita perderia justamente o log do
// incidente.

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
	"github.com/marcoscorrea/ndovu/backend/internal/platform"
)

// LogIngestService recebe logs já decodificados do OTLP, redige atributos e
// publica no stream. Nunca espera o banco.
type LogIngestService struct {
	stream domain.LogStream
	logger *slog.Logger
}

// NewLogIngestService cria o serviço.
func NewLogIngestService(stream domain.LogStream, logger *slog.Logger) *LogIngestService {
	return &LogIngestService{stream: stream, logger: logger}
}

// Ingest redige e enfileira os logs de um app.
func (s *LogIngestService) Ingest(ctx context.Context, app string, logs []domain.LogRecord) error {
	if len(logs) == 0 {
		return nil
	}
	for i := range logs {
		RedactLog(&logs[i])
	}
	if err := s.stream.PublishLogs(ctx, domain.LogBatch{App: app, Logs: logs}); err != nil {
		return fmt.Errorf("publicando logs no stream: %w", err)
	}
	s.logger.DebugContext(ctx, "logs enfileirados", "app", app, "logs", len(logs))
	return nil
}

// RedactLog aplica a mesma lista de chaves sensíveis dos spans aos atributos
// do log. O corpo é texto livre do cliente: não dá para garantir redação sem
// falso positivo, então a orientação é não logar segredo (o SDK e o OTel já
// redigem headers conhecidos).
func RedactLog(l *domain.LogRecord) {
	redactAttrs(l.Attrs)
	redactAttrs(l.ResourceAttrs)
}

// LogDeadLetterSink recebe lotes de logs que o armazém rejeita.
type LogDeadLetterSink func(ctx context.Context, batch domain.LogBatch, reason string) error

// LogWriteService persiste os lotes de logs vindos do stream.
type LogWriteService struct {
	writer domain.LogWriter
	dlq    LogDeadLetterSink
	logger *slog.Logger
}

// NewLogWriteService cria o serviço.
func NewLogWriteService(writer domain.LogWriter, dlq LogDeadLetterSink, logger *slog.Logger) *LogWriteService {
	return &LogWriteService{writer: writer, dlq: dlq, logger: logger}
}

// HandleBatches grava tudo num INSERT só; se falhar, tenta lote a lote e manda
// o que falhar para a DLQ — mesma estratégia dos spans.
func (s *LogWriteService) HandleBatches(ctx context.Context, batches []domain.LogBatch) error {
	ctx = platform.WithoutTracing(ctx)
	if len(batches) == 0 {
		return nil
	}
	if _, err := s.writer.SaveLogs(ctx, batches); err == nil {
		return nil
	} else if len(batches) == 1 {
		return s.quarantine(ctx, batches[0], err)
	} else {
		s.logger.WarnContext(ctx, "insert combinado de logs falhou; tentando lote a lote", "err", err)
	}
	for _, b := range batches {
		if _, err := s.writer.SaveLogs(ctx, []domain.LogBatch{b}); err != nil {
			if qErr := s.quarantine(ctx, b, err); qErr != nil {
				return qErr
			}
		}
	}
	return nil
}

func (s *LogWriteService) quarantine(ctx context.Context, b domain.LogBatch, cause error) error {
	s.logger.ErrorContext(ctx, "lote de logs rejeitado pelo armazém — enviando para DLQ",
		"app", b.App, "logs", len(b.Logs), "err", cause)
	if s.dlq == nil {
		return fmt.Errorf("persistindo logs do app %s: %w", b.App, cause)
	}
	return s.dlq(ctx, b, cause.Error())
}
