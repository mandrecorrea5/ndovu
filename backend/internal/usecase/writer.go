package usecase

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// DeadLetterSink recebe lotes que falham de forma permanente (dados que o
// armazém rejeita), para não travarem a fila e não serem perdidos.
type DeadLetterSink interface {
	DeadLetter(ctx context.Context, batch domain.IngestBatch, reason string) error
}

// SamplingFilter aplica regras de sampling antes do write. Interface
// opcional — WriteService funciona sem ela (mantém tudo).
type SamplingFilter interface {
	FilterBatch(batch domain.IngestBatch) domain.IngestBatch
}

// WriteService é o lado consumidor do pipeline: recebe lotes do stream e os
// persiste em bulk no armazenamento analítico. Um erro aqui faz o stream
// reentregar (at-least-once) — a deduplicação por eventId torna isso seguro.
type WriteService struct {
	writer   domain.EventWriter
	dlq      DeadLetterSink
	sampling SamplingFilter // opcional; nil = sem sampling
	logger   *slog.Logger
}

// NewWriteService cria o serviço do writer.
func NewWriteService(writer domain.EventWriter, dlq DeadLetterSink, logger *slog.Logger) *WriteService {
	return &WriteService{writer: writer, dlq: dlq, logger: logger}
}

// WithSampling pluga um filtro que roda antes do persist. Chamado só uma
// vez no wiring; race não é preocupação.
func (s *WriteService) WithSampling(f SamplingFilter) *WriteService {
	s.sampling = f
	return s
}

// HandleBatches persiste um conjunto de lotes em um único insert. Se o insert
// combinado falhar, cai para lote-a-lote: falha individual vai para a DLQ
// (mensagem venenosa não pode travar a fila), o resto segue.
func (s *WriteService) HandleBatches(ctx context.Context, batches []domain.IngestBatch) error {
	if len(batches) == 0 {
		return nil
	}
	// Sampling roda antes do insert. Batches que ficam vazios após o filtro
	// são descartados (dropados são só métrica, não vão nem pra DLQ).
	if s.sampling != nil {
		filtered := batches[:0]
		for _, b := range batches {
			f := s.sampling.FilterBatch(b)
			if len(f.Events) > 0 {
				filtered = append(filtered, f)
			}
		}
		batches = filtered
		if len(batches) == 0 {
			return nil
		}
	}
	res, err := s.writer.SaveBatches(ctx, batches)
	if err == nil {
		s.logger.InfoContext(ctx, "lotes persistidos", "batches", len(batches), "events", res.Accepted)
		return nil
	}
	if len(batches) == 1 {
		return s.quarantine(ctx, batches[0], err)
	}

	s.logger.WarnContext(ctx, "insert combinado falhou; tentando lote a lote", "err", err)
	for _, b := range batches {
		if _, err := s.writer.SaveBatches(ctx, []domain.IngestBatch{b}); err != nil {
			if qErr := s.quarantine(ctx, b, err); qErr != nil {
				return qErr
			}
		}
	}
	return nil
}

// quarantine manda o lote para a DLQ; se nem isso for possível (ex.: stream
// fora do ar), devolve erro para o lote ser reentregue.
func (s *WriteService) quarantine(ctx context.Context, b domain.IngestBatch, cause error) error {
	s.logger.ErrorContext(ctx, "lote rejeitado pelo armazém — enviando para DLQ",
		"app", b.Session.App,
		"session_id", b.Session.SessionID,
		"events", len(b.Events),
		"err", cause,
	)
	if s.dlq == nil {
		return fmt.Errorf("persistindo lote (sem DLQ configurada): %w", cause)
	}
	if err := s.dlq.DeadLetter(ctx, b, cause.Error()); err != nil {
		return fmt.Errorf("enviando lote para DLQ: %w", err)
	}
	return nil
}
