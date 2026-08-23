package domain

import "testing"

func TestFingerprintAgrupaVariantesDaMesmaFalha(t *testing.T) {
	base := TraceEvent{
		App:  "portal",
		Type: EventError,
		Name: "falha_login",
		Error: &ErrorInfo{
			Code:    "AUTH_FAIL",
			Message: "usuário 12345 inválido",
		},
	}
	variante := base
	variante.Error = &ErrorInfo{Code: "auth_fail", Message: "USUÁRIO 67890 INVÁLIDO"}

	if Fingerprint(base) != Fingerprint(variante) {
		t.Fatalf("fingerprint deveria agrupar variantes: base=%s var=%s",
			Fingerprint(base), Fingerprint(variante))
	}
}

func TestFingerprintNormalizaRotasComID(t *testing.T) {
	status := 500
	a := TraceEvent{
		App:  "api",
		Type: EventError,
		Error: &ErrorInfo{Code: "HTTP_500", Message: "server error"},
		HTTP: &HTTPInfo{Method: "GET", URL: "/users/abcdef01-2345-6789/orders", StatusCode: &status},
	}
	b := a
	b.HTTP = &HTTPInfo{Method: "GET", URL: "/users/fedcba98-7654-3210/orders?ref=x", StatusCode: &status}

	if Fingerprint(a) != Fingerprint(b) {
		t.Fatalf("mesma rota com IDs diferentes deveria mesmo fingerprint")
	}
}

func TestFingerprintDiferenciaAppsDistintos(t *testing.T) {
	a := TraceEvent{App: "portal", Type: EventError, Error: &ErrorInfo{Code: "X", Message: "boom"}}
	b := TraceEvent{App: "admin", Type: EventError, Error: &ErrorInfo{Code: "X", Message: "boom"}}
	if Fingerprint(a) == Fingerprint(b) {
		t.Fatalf("apps diferentes não deveriam colidir")
	}
}

func TestFingerprintVazioParaEventoSemErro(t *testing.T) {
	e := TraceEvent{App: "portal", Type: EventAction, Name: "clicou_submit"}
	if Fingerprint(e) != "" {
		t.Fatalf("evento sem erro não deveria ter fingerprint")
	}
}
