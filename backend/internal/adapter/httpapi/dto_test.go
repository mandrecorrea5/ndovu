package httpapi

import "testing"

// O `sdkVersion` era parseado do envelope e silenciosamente perdido no
// toDomain() — nenhum teste cobria a fronteira DTO→domínio, e foi por isso que
// passou despercebido por todo o histórico do repositório.
func TestToDomainPropagaSDKVersion(t *testing.T) {
	req := ingestRequest{
		App:        "portal",
		SDKVersion: "1.3.0",
		Session:    sessionDTO{SessionID: "s-1", UserID: "u-1"},
	}

	batch := req.toDomain()

	if batch.SDKVersion != "1.3.0" {
		t.Fatalf("sdkVersion perdido no toDomain(): %+v", batch)
	}
	if batch.Session.SessionID != "s-1" || batch.Session.App != "portal" {
		t.Fatalf("sessão montada incorretamente: %+v", batch.Session)
	}
}
