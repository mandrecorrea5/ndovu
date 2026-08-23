// Package usecase orquestra as regras de aplicação sobre o domínio.
package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// MaxBatchEvents limita o tamanho de um lote de ingestão.
const MaxBatchEvents = 500

// IngestService valida lotes de traces vindos dos frontends e os publica no
// stream durável. A persistência acontece de forma assíncrona no writer —
// a ingestão responde em milissegundos e nunca depende do banco.
type IngestService struct {
	stream domain.EventStream
	logger *slog.Logger
	now    func() time.Time
}

// NewIngestService cria o serviço de ingestão.
func NewIngestService(stream domain.EventStream, logger *slog.Logger) *IngestService {
	return &IngestService{stream: stream, logger: logger, now: time.Now}
}

// Ingest valida o lote contra o contrato v1 e o enfileira no stream.
// Accepted = eventos enfileirados; a deduplicação por eventId acontece no
// armazenamento (reenvios são seguros).
func (s *IngestService) Ingest(ctx context.Context, batch domain.IngestBatch) (domain.IngestResult, error) {
	if err := validateBatch(batch); err != nil {
		return domain.IngestResult{}, err
	}

	now := s.now().UTC()
	normalizeBatch(&batch, now)

	if err := s.stream.Publish(ctx, batch); err != nil {
		return domain.IngestResult{}, fmt.Errorf("publicando lote no stream: %w", err)
	}

	s.logger.InfoContext(ctx, "lote enfileirado",
		"app", batch.Session.App,
		"session_id", batch.Session.SessionID,
		"events", len(batch.Events),
	)
	return domain.IngestResult{Accepted: len(batch.Events)}, nil
}

func validateBatch(b domain.IngestBatch) error {
	var issues []string
	if b.Session.App == "" {
		issues = append(issues, "app é obrigatório")
	}
	if b.Session.SessionID == "" {
		issues = append(issues, "session.sessionId é obrigatório")
	}
	if len(b.Events) == 0 {
		issues = append(issues, "events não pode ser vazio")
	}
	if len(b.Events) > MaxBatchEvents {
		issues = append(issues, fmt.Sprintf("events excede o máximo de %d por lote", MaxBatchEvents))
	}
	for i, e := range b.Events {
		if e.ID == "" {
			issues = append(issues, fmt.Sprintf("events[%d].eventId é obrigatório", i))
		} else if _, err := uuid.Parse(e.ID); err != nil {
			// UUID é exigido pelo contrato: garante idempotência global e é o
			// tipo da coluna no armazém — rejeitar aqui evita lote venenoso.
			issues = append(issues, fmt.Sprintf("events[%d].eventId deve ser um UUID válido", i))
		}
		if !e.Type.Valid() {
			issues = append(issues, fmt.Sprintf("events[%d].type %q inválido", i, e.Type))
		}
		if e.Name == "" {
			issues = append(issues, fmt.Sprintf("events[%d].name é obrigatório", i))
		}
		if e.OccurredAt.IsZero() {
			issues = append(issues, fmt.Sprintf("events[%d].timestamp é obrigatório (RFC3339)", i))
		}
	}
	if len(issues) > 0 {
		return domain.NewValidationError(issues...)
	}
	return nil
}

// normalizeBatch propaga sessão/app/usuário para os eventos e calcula os
// timestamps agregados da sessão. Também extrai `release` de
// session.attributes.release quando o evento não trouxe a coluna própria —
// SDKs antigos enviam via atributos.
func normalizeBatch(b *domain.IngestBatch, now time.Time) {
	first, last := now, now
	sessionRelease := extractRelease(b.Session.Attributes)
	for i := range b.Events {
		e := &b.Events[i]
		e.SessionID = b.Session.SessionID
		e.App = b.Session.App
		if e.UserID == "" {
			e.UserID = b.Session.UserID
		}
		if e.Release == "" {
			e.Release = sessionRelease
		}
		e.OccurredAt = e.OccurredAt.UTC()
		e.ReceivedAt = now
		if i == 0 || e.OccurredAt.Before(first) {
			first = e.OccurredAt
		}
		if i == 0 || e.OccurredAt.After(last) {
			last = e.OccurredAt
		}
	}
	b.Session.StartedAt = first
	b.Session.LastEventAt = last
	if len(b.Session.Attributes) == 0 {
		b.Session.Attributes = []byte(`{}`)
	}
}

// extractRelease lê "release" do JSON de atributos da sessão. Retorna string
// vazia se ausente ou malformado — release é opcional.
func extractRelease(attrs json.RawMessage) string {
	if len(attrs) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(attrs, &m); err != nil {
		return ""
	}
	if v, ok := m["release"].(string); ok {
		return v
	}
	return ""
}
