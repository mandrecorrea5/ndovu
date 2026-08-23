package usecase

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

type fakeStream struct {
	published []domain.IngestBatch
	fail      error
}

func (f *fakeStream) Publish(_ context.Context, b domain.IngestBatch) error {
	if f.fail != nil {
		return f.fail
	}
	f.published = append(f.published, b)
	return nil
}

func validBatch() domain.IngestBatch {
	return domain.IngestBatch{
		Session: domain.Session{SessionID: "s-1", UserID: "u-1", App: "portal"},
		Events: []domain.TraceEvent{{
			ID:         "0d9e0a44-8c2b-4c33-a1f2-7e6b5d4c3b2a",
			Type:       domain.EventHTTPRequest,
			Name:       "login_success",
			OccurredAt: time.Now(),
		}},
	}
}

func TestIngestValido(t *testing.T) {
	stream := &fakeStream{}
	svc := NewIngestService(stream, slog.Default())

	res, err := svc.Ingest(context.Background(), validBatch())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.Accepted != 1 {
		t.Fatalf("accepted = %d, esperado 1", res.Accepted)
	}
	if len(stream.published) != 1 {
		t.Fatalf("lote não publicado no stream")
	}
	got := stream.published[0].Events[0]
	if got.SessionID != "s-1" || got.App != "portal" || got.UserID != "u-1" {
		t.Fatalf("sessão/app/usuário não propagados: %+v", got)
	}
}

func TestIngestFalhaDoStream(t *testing.T) {
	stream := &fakeStream{fail: errors.New("nats indisponível")}
	svc := NewIngestService(stream, slog.Default())

	_, err := svc.Ingest(context.Background(), validBatch())
	if err == nil {
		t.Fatal("esperado erro quando o stream está indisponível")
	}
}

func TestIngestContratoViolado(t *testing.T) {
	cases := map[string]func(*domain.IngestBatch){
		"sem app":          func(b *domain.IngestBatch) { b.Session.App = "" },
		"sem sessionId":    func(b *domain.IngestBatch) { b.Session.SessionID = "" },
		"sem eventos":      func(b *domain.IngestBatch) { b.Events = nil },
		"sem eventId":      func(b *domain.IngestBatch) { b.Events[0].ID = "" },
		"tipo inválido":    func(b *domain.IngestBatch) { b.Events[0].Type = "invalido" },
		"eventId não-uuid": func(b *domain.IngestBatch) { b.Events[0].ID = "abc-123" },
		"sem name":         func(b *domain.IngestBatch) { b.Events[0].Name = "" },
		"sem timestamp":    func(b *domain.IngestBatch) { b.Events[0].OccurredAt = time.Time{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			b := validBatch()
			mutate(&b)
			_, err := NewIngestService(&fakeStream{}, slog.Default()).Ingest(context.Background(), b)
			if !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("esperado ErrValidation, veio: %v", err)
			}
		})
	}
}

func TestIngestLoteGrandeDemais(t *testing.T) {
	b := validBatch()
	ev := b.Events[0]
	for i := 0; i < MaxBatchEvents+1; i++ {
		b.Events = append(b.Events, ev)
	}
	_, err := NewIngestService(&fakeStream{}, slog.Default()).Ingest(context.Background(), b)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("esperado ErrValidation, veio: %v", err)
	}
}

type fakeWriter struct {
	batches [][]domain.IngestBatch
}

func (f *fakeWriter) SaveBatches(_ context.Context, b []domain.IngestBatch) (domain.IngestResult, error) {
	f.batches = append(f.batches, b)
	total := 0
	for _, batch := range b {
		total += len(batch.Events)
	}
	return domain.IngestResult{Accepted: total}, nil
}

func TestWriterHandleBatches(t *testing.T) {
	w := &fakeWriter{}
	svc := NewWriteService(w, nil, slog.Default())
	if err := svc.HandleBatches(context.Background(), []domain.IngestBatch{validBatch(), validBatch()}); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(w.batches) != 1 || len(w.batches[0]) != 2 {
		t.Fatalf("esperado 1 chamada com 2 lotes, veio %+v", w.batches)
	}
	if err := svc.HandleBatches(context.Background(), nil); err != nil {
		t.Fatalf("lote vazio deve ser no-op: %v", err)
	}
}
