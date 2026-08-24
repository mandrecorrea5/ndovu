package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// validEventEnvelope monta um envelope válido do contrato v1 para o app dado.
func validEventEnvelope(app string) map[string]any {
	return map[string]any{
		"app":        app,
		"sdkVersion": "1.0",
		"session": map[string]any{
			"sessionId": "11111111-1111-4111-8111-111111111111",
			"userId":    "u-1",
		},
		"events": []map[string]any{
			{
				"eventId":   "22222222-2222-4222-8222-222222222222",
				"type":      "action",
				"name":      "smoke",
				"timestamp": time.Now().UTC().Format(time.RFC3339),
			},
		},
	}
}

// -----------------------------------------------------------------------
// POST /v1/events — happy path e guard cross-tenant
// -----------------------------------------------------------------------

func TestIngest_HappyPath202(t *testing.T) {
	f := newFixture(t)
	f.seedApp("portal", "c-default")
	key := f.seedKey("portal")

	rr := f.do(f.keyRequest(http.MethodPost, "/v1/events", validEventEnvelope("portal"), key))
	requireStatus(t, rr, http.StatusAccepted)

	// Publicou no stream.
	if len(f.stream.published) != 1 {
		t.Errorf("esperava 1 lote no stream, veio %d", len(f.stream.published))
	}
}

func TestIngest_SemChave401(t *testing.T) {
	f := newFixture(t)
	rr := f.do(f.req(http.MethodPost, "/v1/events", validEventEnvelope("portal")))
	requireStatus(t, rr, http.StatusUnauthorized)
}

func TestIngest_ChaveInvalida401(t *testing.T) {
	f := newFixture(t)
	rr := f.do(f.keyRequest(http.MethodPost, "/v1/events",
		validEventEnvelope("portal"), "ndk_chave-inexistente"))
	requireStatus(t, rr, http.StatusUnauthorized)
}

func TestIngest_CrossTenantGuardApp400(t *testing.T) {
	// Chave é do app "portal-a"; envelope diz app "portal-b" → 400.
	f := newFixture(t)
	f.seedApp("portal-a", "c-default")
	f.seedApp("portal-b", "c-default")
	key := f.seedKey("portal-a")

	rr := f.do(f.keyRequest(http.MethodPost, "/v1/events",
		validEventEnvelope("portal-b"), key))
	requireStatus(t, rr, http.StatusBadRequest)

	var body struct {
		Error   string   `json:"error"`
		Details []string `json:"details"`
	}
	decodeJSON(t, rr, &body)
	if len(body.Details) == 0 {
		t.Errorf("details deveria explicar divergência: %+v", body)
	}
	if len(f.stream.published) != 0 {
		t.Errorf("lote com app divergente NÃO deveria ter sido publicado")
	}
}

func TestIngest_ContratoInvalidoTypeErrado400(t *testing.T) {
	f := newFixture(t)
	f.seedApp("portal", "c-default")
	key := f.seedKey("portal")

	envelope := validEventEnvelope("portal")
	events := envelope["events"].([]map[string]any)
	events[0]["type"] = "click" // inválido — deve ser page_view/action/http_request/error/custom
	envelope["events"] = events

	rr := f.do(f.keyRequest(http.MethodPost, "/v1/events", envelope, key))
	// Pode ser 400 (validação do contrato) — service Ingest valida.
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400 pra type inválido, veio %d: %s", rr.Code, rr.Body.String())
	}
}

func TestIngest_EventIdInvalido400(t *testing.T) {
	f := newFixture(t)
	f.seedApp("portal", "c-default")
	key := f.seedKey("portal")

	envelope := validEventEnvelope("portal")
	events := envelope["events"].([]map[string]any)
	events[0]["eventId"] = "nao-eh-uuid"
	envelope["events"] = events

	rr := f.do(f.keyRequest(http.MethodPost, "/v1/events", envelope, key))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400 pra eventId não-UUID, veio %d: %s", rr.Code, rr.Body.String())
	}
}

func TestIngest_JsonMalformado400(t *testing.T) {
	f := newFixture(t)
	f.seedApp("portal", "c-default")
	key := f.seedKey("portal")

	req := httptest.NewRequest(http.MethodPost, "/v1/events",
		strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", key)
	rr := f.do(req)
	requireStatus(t, rr, http.StatusBadRequest)
}

func TestIngest_PayloadGigante413(t *testing.T) {
	// Config.MaxBodyBytes = 1MB no fixture. Enviando 2MB dispara maxBody.
	f := newFixture(t)
	f.seedApp("portal", "c-default")
	key := f.seedKey("portal")

	// Body válido em estrutura mas payload gigante em metadata.
	big := strings.Repeat("x", 2*1024*1024)
	env := validEventEnvelope("portal")
	events := env["events"].([]map[string]any)
	events[0]["metadata"] = map[string]any{"blob": big}
	env["events"] = events
	body, _ := json.Marshal(env)

	req := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", key)
	rr := f.do(req)
	if rr.Code != http.StatusRequestEntityTooLarge && rr.Code != http.StatusBadRequest {
		t.Fatalf("payload > 1MB deveria dar 413 (ou 400 de decode), veio %d", rr.Code)
	}
}

// -----------------------------------------------------------------------
// POST /v1/feedbacks — mesma chave, valida cross-tenant
// -----------------------------------------------------------------------

func TestFeedback_IngestHappyPath(t *testing.T) {
	f := newFixture(t)
	f.seedApp("portal", "c-default")
	key := f.seedKey("portal")

	rr := f.do(f.keyRequest(http.MethodPost, "/v1/feedbacks", map[string]any{
		"app":       "portal",
		"sessionId": "sess-1",
		"type":      "bug",
		"message":   "botão não funciona",
	}, key))
	if rr.Code != http.StatusOK && rr.Code != http.StatusCreated && rr.Code != http.StatusAccepted {
		t.Fatalf("esperava 2xx, veio %d: %s", rr.Code, rr.Body.String())
	}
	if len(f.feedback.items) != 1 {
		t.Errorf("feedback não persistiu")
	}
}

func TestFeedback_IngestCrossTenantGuard400(t *testing.T) {
	f := newFixture(t)
	f.seedApp("portal-a", "c-default")
	f.seedApp("portal-b", "c-default")
	key := f.seedKey("portal-a")

	rr := f.do(f.keyRequest(http.MethodPost, "/v1/feedbacks", map[string]any{
		"app":       "portal-b",
		"sessionId": "s",
		"message":   "x",
	}, key))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400 pra app divergente, veio %d: %s", rr.Code, rr.Body.String())
	}
	if len(f.feedback.items) != 0 {
		t.Errorf("feedback não deveria ter sido persistido")
	}
}
