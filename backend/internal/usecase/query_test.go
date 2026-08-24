package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// -- clampLimit --------------------------------------------------------

func TestClampLimit(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, defaultPageSize},
		{-1, defaultPageSize},
		{-100, defaultPageSize},
		{25, 25},
		{maxPageSize, maxPageSize},
		{maxPageSize + 1, maxPageSize},
		{99999, maxPageSize},
	}
	for _, c := range cases {
		if got := clampLimit(c.in); got != c.want {
			t.Errorf("clampLimit(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

// -- fakeQueryReader captura filtros pra assertion ---------------------

type fakeQueryReader struct {
	nopEventReader
	lastEventFilter   domain.EventFilter
	lastSessionFilter domain.SessionFilter
	getSessionErr     error
	sessionToReturn   domain.Session
	timelineToReturn  []domain.TraceEvent
}

func (r *fakeQueryReader) FindEvents(_ context.Context, f domain.EventFilter) (domain.EventPage, error) {
	r.lastEventFilter = f
	return domain.EventPage{}, nil
}
func (r *fakeQueryReader) FindSessions(_ context.Context, f domain.SessionFilter) (domain.SessionPage, error) {
	r.lastSessionFilter = f
	return domain.SessionPage{}, nil
}
func (r *fakeQueryReader) GetSession(context.Context, string) (domain.Session, error) {
	if r.getSessionErr != nil {
		return domain.Session{}, r.getSessionErr
	}
	return r.sessionToReturn, nil
}
func (r *fakeQueryReader) SessionTimeline(context.Context, string) ([]domain.TraceEvent, error) {
	return r.timelineToReturn, nil
}

// -- Events aplica clampLimit -----------------------------------------

func TestQuery_EventsClampaLimit(t *testing.T) {
	reader := &fakeQueryReader{}
	svc := NewQueryService(reader)
	_, _ = svc.Events(context.Background(), domain.EventFilter{Limit: 0})
	if reader.lastEventFilter.Limit != defaultPageSize {
		t.Errorf("esperava %d, veio %d", defaultPageSize, reader.lastEventFilter.Limit)
	}
	_, _ = svc.Events(context.Background(), domain.EventFilter{Limit: 999})
	if reader.lastEventFilter.Limit != maxPageSize {
		t.Errorf("esperava %d, veio %d", maxPageSize, reader.lastEventFilter.Limit)
	}
}

func TestQuery_SessionsClampaLimit(t *testing.T) {
	reader := &fakeQueryReader{}
	svc := NewQueryService(reader)
	_, _ = svc.Sessions(context.Background(), domain.SessionFilter{Limit: 0})
	if reader.lastSessionFilter.Limit != defaultPageSize {
		t.Errorf("esperava %d, veio %d", defaultPageSize, reader.lastSessionFilter.Limit)
	}
}

// -- Event valida id --------------------------------------------------

func TestQuery_EventIdVazioRetornaValidation(t *testing.T) {
	svc := NewQueryService(&fakeQueryReader{})
	_, err := svc.Event(context.Background(), "")
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("esperava ValidationError, veio %v", err)
	}
}

// -- Session (happy path) ---------------------------------------------

func TestQuery_SessionHappyPath(t *testing.T) {
	reader := &fakeQueryReader{
		sessionToReturn: domain.Session{
			SessionID: "s-1", App: "portal", UserID: "u-1",
			EventCount: 5, StartedAt: time.Now(),
		},
		timelineToReturn: []domain.TraceEvent{
			{ID: "e-1", SessionID: "s-1", App: "portal"},
			{ID: "e-2", SessionID: "s-1", App: "portal"},
		},
	}
	svc := NewQueryService(reader)

	got, err := svc.Session(context.Background(), "s-1")
	if err != nil {
		t.Fatalf("session falhou: %v", err)
	}
	if got.Session.SessionID != "s-1" {
		t.Errorf("session errada: %+v", got.Session)
	}
	if len(got.Timeline) != 2 {
		t.Errorf("timeline errada: %d eventos", len(got.Timeline))
	}
}

func TestQuery_SessionIdVazioRetornaValidation(t *testing.T) {
	svc := NewQueryService(&fakeQueryReader{})
	_, err := svc.Session(context.Background(), "")
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("esperava ValidationError, veio %v", err)
	}
}

// -- Session fallback via feedback -----------------------------------

func TestQuery_SessionNotFoundSemFallbackRetornaNotFound(t *testing.T) {
	reader := &fakeQueryReader{getSessionErr: domain.ErrNotFound}
	svc := NewQueryService(reader) // sem WithFeedbackFallback
	_, err := svc.Session(context.Background(), "s-inexistente")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("sem fallback + not found deveria propagar ErrNotFound, veio %v", err)
	}
}

func TestQuery_SessionNotFoundComFallbackFeedbackMaterializaShell(t *testing.T) {
	// ClickHouse não tem a sessão, mas o FeedbackStore tem — devolve
	// shell com app+userId do feedback e timeline vazia.
	reader := &fakeQueryReader{getSessionErr: domain.ErrNotFound}
	fbStore := &fakeFeedbackStore{}
	now := time.Now().UTC()
	fbStore.items = append(fbStore.items, domain.UserFeedback{
		ID: "fb-1", App: "portal-feedback-only", SessionID: "s-feedback",
		UserID: "u-feedback", CreatedAt: now,
	})

	svc := NewQueryService(reader).WithFeedbackFallback(fbStore)
	got, err := svc.Session(context.Background(), "s-feedback")
	if err != nil {
		t.Fatalf("fallback deveria funcionar, veio: %v", err)
	}
	if got.Session.App != "portal-feedback-only" {
		t.Errorf("app do feedback não veio: %+v", got.Session)
	}
	if got.Session.UserID != "u-feedback" {
		t.Errorf("userId do feedback não veio: %+v", got.Session)
	}
	if got.Timeline == nil {
		t.Error("timeline não deveria ser nil (regressão do bug retention)")
	}
	if len(got.Timeline) != 0 {
		t.Errorf("timeline deveria ser vazia, veio %d", len(got.Timeline))
	}
}

func TestQuery_SessionNotFoundComFallbackMasFeedbackTambemVazio(t *testing.T) {
	reader := &fakeQueryReader{getSessionErr: domain.ErrNotFound}
	fbStore := &fakeFeedbackStore{} // vazio
	svc := NewQueryService(reader).WithFeedbackFallback(fbStore)
	_, err := svc.Session(context.Background(), "s-nada")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("sem feedback nem sessão, esperava ErrNotFound, veio %v", err)
	}
}

func TestQuery_SessionErroOutroQueNotFoundNaoUsaFallback(t *testing.T) {
	// Bug se pegasse fallback aqui: um erro real (ex.: timeout) seria
	// silenciosamente convertido em "sessão do feedback".
	fatalErr := errors.New("timeout do banco")
	reader := &fakeQueryReader{getSessionErr: fatalErr}
	fbStore := &fakeFeedbackStore{}
	fbStore.items = append(fbStore.items, domain.UserFeedback{SessionID: "s-1", App: "a"})
	svc := NewQueryService(reader).WithFeedbackFallback(fbStore)

	_, err := svc.Session(context.Background(), "s-1")
	if err != fatalErr {
		t.Errorf("erro real deveria propagar, veio: %v", err)
	}
}
