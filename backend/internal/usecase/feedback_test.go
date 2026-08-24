package usecase

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

type fakeFeedbackStore struct {
	items []domain.UserFeedback
	seq   int
}

func (f *fakeFeedbackStore) CreateFeedback(_ context.Context, fb domain.UserFeedback) (domain.UserFeedback, error) {
	f.seq++
	fb.ID = string(rune('A' + f.seq))
	fb.Status = domain.FeedbackNew
	fb.CreatedAt = time.Now().UTC()
	f.items = append(f.items, fb)
	return fb, nil
}
func (f *fakeFeedbackStore) GetFeedback(_ context.Context, id string) (domain.UserFeedback, error) {
	for _, it := range f.items {
		if it.ID == id {
			return it, nil
		}
	}
	return domain.UserFeedback{}, domain.ErrNotFound
}
func (f *fakeFeedbackStore) UpdateFeedbackStatus(_ context.Context, id string, status domain.FeedbackStatus, by string) (domain.UserFeedback, error) {
	for i, it := range f.items {
		if it.ID == id {
			f.items[i].Status = status
			f.items[i].ResolvedBy = by
			return f.items[i], nil
		}
	}
	return domain.UserFeedback{}, domain.ErrNotFound
}
func (f *fakeFeedbackStore) ListFeedbacks(_ context.Context, filter domain.FeedbackFilter) ([]domain.UserFeedback, int, error) {
	out := []domain.UserFeedback{}
	for _, it := range f.items {
		if filter.App != "" && it.App != filter.App {
			continue
		}
		if filter.Status != "" && string(it.Status) != filter.Status {
			continue
		}
		if filter.SessionID != "" && it.SessionID != filter.SessionID {
			continue
		}
		out = append(out, it)
	}
	return out, len(out), nil
}
func (f *fakeFeedbackStore) DeleteFeedback(_ context.Context, id string) error {
	for i, it := range f.items {
		if it.ID == id {
			f.items = append(f.items[:i], f.items[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

func newFeedbackSvc() (*FeedbackService, *fakeFeedbackStore) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := &fakeFeedbackStore{}
	return NewFeedbackService(store, logger), store
}

func TestFeedback_IngestValidaCamposObrigatorios(t *testing.T) {
	svc, _ := newFeedbackSvc()
	cases := []struct {
		name string
		in   FeedbackInput
	}{
		{"app vazio", FeedbackInput{SessionID: "s", Message: "m"}},
		{"session vazio", FeedbackInput{App: "a", Message: "m"}},
		{"message vazia", FeedbackInput{App: "a", SessionID: "s"}},
		{"message só espaço em branco", FeedbackInput{App: "a", SessionID: "s", Message: "   "}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := svc.Ingest(context.Background(), c.in)
			if err == nil {
				t.Fatal("esperava validação falhar")
			}
			var ve *domain.ValidationError
			if !errors.As(err, &ve) {
				t.Errorf("esperava ValidationError, veio %T: %v", err, err)
			}
		})
	}
}

func TestFeedback_IngestMessageAcimaDeLimiteFalha(t *testing.T) {
	svc, _ := newFeedbackSvc()
	tooLong := strings.Repeat("x", 5001)
	_, err := svc.Ingest(context.Background(), FeedbackInput{
		App: "a", SessionID: "s", Message: tooLong,
	})
	if err == nil {
		t.Fatal("esperava erro por message > 5000 chars")
	}
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("esperava ValidationError, veio %T", err)
	}
	if !strings.Contains(strings.Join(ve.Issues, "|"), "5000") {
		t.Errorf("issues não mencionam limite: %v", ve.Issues)
	}
}

func TestFeedback_IngestTypeDefault(t *testing.T) {
	// Type vazio deve virar "bug" (default).
	svc, store := newFeedbackSvc()
	_, err := svc.Ingest(context.Background(), FeedbackInput{
		App: "a", SessionID: "s", Message: "m",
	})
	if err != nil {
		t.Fatalf("ingest falhou: %v", err)
	}
	if store.items[0].Type != domain.FeedbackBug {
		t.Errorf("type default deveria ser bug, veio %q", store.items[0].Type)
	}
}

func TestFeedback_IngestTypeInvalido(t *testing.T) {
	svc, _ := newFeedbackSvc()
	_, err := svc.Ingest(context.Background(), FeedbackInput{
		App: "a", SessionID: "s", Message: "m", Type: "invalid-type",
	})
	if err == nil {
		t.Fatal("esperava erro em type inválido")
	}
}

func TestFeedback_IngestPersisteStatusNew(t *testing.T) {
	svc, store := newFeedbackSvc()
	fb, err := svc.Ingest(context.Background(), FeedbackInput{
		App: "portal", SessionID: "s-1", Message: "botão quebrado",
		Type: domain.FeedbackBug, Email: "cliente@x.com",
	})
	if err != nil {
		t.Fatalf("ingest falhou: %v", err)
	}
	if fb.Status != domain.FeedbackNew {
		t.Errorf("status inicial deveria ser 'new', veio %q", fb.Status)
	}
	if len(store.items) != 1 {
		t.Fatalf("esperava 1 item persistido, veio %d", len(store.items))
	}
}

func TestFeedback_IngestTrimMensagem(t *testing.T) {
	// Mensagem com espaço em branco antes/depois deve ser normalizada.
	svc, _ := newFeedbackSvc()
	fb, err := svc.Ingest(context.Background(), FeedbackInput{
		App: "a", SessionID: "s", Message: "   texto útil   ",
	})
	if err != nil {
		t.Fatalf("ingest falhou: %v", err)
	}
	if fb.Message != "texto útil" {
		t.Errorf("message não foi trimmed: %q", fb.Message)
	}
}

func TestFeedback_SetStatusInvalidoFalha(t *testing.T) {
	svc, _ := newFeedbackSvc()
	fb, _ := svc.Ingest(context.Background(), FeedbackInput{
		App: "a", SessionID: "s", Message: "m",
	})
	_, err := svc.SetStatus(context.Background(), fb.ID, "algum-status-esquisito", "actor")
	if err == nil {
		t.Fatal("esperava erro em status inválido")
	}
}

func TestFeedback_SetStatusValidoAtualizaEArmazenaActor(t *testing.T) {
	svc, store := newFeedbackSvc()
	fb, _ := svc.Ingest(context.Background(), FeedbackInput{
		App: "a", SessionID: "s", Message: "m",
	})
	updated, err := svc.SetStatus(context.Background(), fb.ID, domain.FeedbackResolved, "user-triagem")
	if err != nil {
		t.Fatalf("setStatus falhou: %v", err)
	}
	if updated.Status != domain.FeedbackResolved {
		t.Errorf("status não atualizou: %q", updated.Status)
	}
	if store.items[0].ResolvedBy != "user-triagem" {
		t.Errorf("resolvedBy não persistiu: %q", store.items[0].ResolvedBy)
	}
}

func TestFeedback_ListFiltraPorSessionID(t *testing.T) {
	// Cobre o filtro adicionado no bloco de cross-tenant (fallback session).
	svc, _ := newFeedbackSvc()
	ctx := context.Background()
	_, _ = svc.Ingest(ctx, FeedbackInput{App: "a", SessionID: "sess-1", Message: "m"})
	_, _ = svc.Ingest(ctx, FeedbackInput{App: "a", SessionID: "sess-2", Message: "m"})

	list, total, err := svc.List(ctx, domain.FeedbackFilter{SessionID: "sess-1"})
	if err != nil {
		t.Fatalf("list falhou: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].SessionID != "sess-1" {
		t.Errorf("esperava só o de sess-1, veio %+v (total=%d)", list, total)
	}
}

func TestFeedback_DeleteRemove(t *testing.T) {
	svc, store := newFeedbackSvc()
	fb, _ := svc.Ingest(context.Background(), FeedbackInput{
		App: "a", SessionID: "s", Message: "m",
	})
	if err := svc.Delete(context.Background(), fb.ID); err != nil {
		t.Fatalf("delete falhou: %v", err)
	}
	if len(store.items) != 0 {
		t.Errorf("esperava 0 items, veio %d", len(store.items))
	}
}
