package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
	"github.com/marcoscorrea/ndovu/backend/internal/usecase"
)

type fakeStream struct {
	published []domain.IngestBatch
}

func (f *fakeStream) Publish(_ context.Context, b domain.IngestBatch) error {
	f.published = append(f.published, b)
	return nil
}

// newTestHandlers monta um Handlers com ingestão sobre um stream fake.
func newTestHandlers() (*Handlers, *fakeStream) {
	stream := &fakeStream{}
	h := &Handlers{
		ingest: usecase.NewIngestService(stream, slog.Default()),
	}
	return h, stream
}

// postEvents executa PostEvents com a chave de API injetada no contexto (como
// faria o middleware apiKeyAuth) e devolve o recorder.
func postEvents(h *Handlers, keyApp, bodyApp string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), apiKeyContextKey,
		domain.APIKey{App: keyApp, Active: true}))
	rr := httptest.NewRecorder()
	h.PostEvents(rr, req)
	return rr
}

func validEventsBody(app string) []byte {
	b, _ := json.Marshal(ingestRequest{
		App: app,
		Session: sessionDTO{
			SessionID: "11111111-1111-4111-8111-111111111111",
		},
		Events: []eventDTO{{
			EventID:   "22222222-2222-4222-8222-222222222222",
			Type:      "action",
			Name:      "smoke_test",
			Timestamp: time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC),
		}},
	})
	return b
}

func TestPostEventsAppBateComChave(t *testing.T) {
	h, stream := newTestHandlers()
	rr := postEvents(h, "portal", "portal", validEventsBody("portal"))
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, esperado 202; body=%s", rr.Code, rr.Body.String())
	}
	if len(stream.published) != 1 {
		t.Fatalf("lote não publicado no stream")
	}
}

func TestPostEventsAppDivergeDaChave(t *testing.T) {
	h, stream := newTestHandlers()
	rr := postEvents(h, "portal", "outro-app", validEventsBody("outro-app"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400; body=%s", rr.Code, rr.Body.String())
	}
	if len(stream.published) != 0 {
		t.Fatalf("lote com app divergente não deveria ser publicado")
	}
	var resp errorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON: %v", err)
	}
	if len(resp.Details) == 0 {
		t.Fatalf("esperado details explicando a divergência de app")
	}
}
