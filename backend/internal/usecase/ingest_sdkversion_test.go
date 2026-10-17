package usecase

import (
	"encoding/json"
	"testing"
	"time"
)

func atributosObj(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("attributes não é objeto JSON: %s (%v)", raw, err)
	}
	return m
}

func TestInjectSDKVersion(t *testing.T) {
	t.Run("envelope vazio não toca nos atributos", func(t *testing.T) {
		b := validBatch()
		b.Session.Attributes = json.RawMessage(`{"release":"1.4.0"}`)

		injectSDKVersion(&b)

		got := atributosObj(t, b.Session.Attributes)
		if len(got) != 1 || got["release"] != "1.4.0" {
			t.Fatalf("atributos alterados sem sdkVersion no envelope: %s", b.Session.Attributes)
		}
	})

	t.Run("normalizeBatch grava quando atributos estão ausentes", func(t *testing.T) {
		b := validBatch()
		b.SDKVersion = "1.3.0"

		normalizeBatch(&b, time.Now())

		if got := atributosObj(t, b.Session.Attributes); got["sdkVersion"] != "1.3.0" {
			t.Fatalf("sdkVersion não gravado: %s", b.Session.Attributes)
		}
	})

	t.Run("preserva atributos que o cliente já enviou", func(t *testing.T) {
		b := validBatch()
		b.SDKVersion = "1.3.0"
		b.Session.Attributes = json.RawMessage(`{"release":"1.4.0","plataforma":"web"}`)

		injectSDKVersion(&b)

		got := atributosObj(t, b.Session.Attributes)
		if got["release"] != "1.4.0" || got["plataforma"] != "web" {
			t.Fatalf("atributos do cliente perdidos: %s", b.Session.Attributes)
		}
		if got["sdkVersion"] != "1.3.0" {
			t.Fatalf("sdkVersion ausente: %s", b.Session.Attributes)
		}
	})

	t.Run("envelope vence sdkVersion posto à mão em attributes", func(t *testing.T) {
		b := validBatch()
		b.SDKVersion = "1.3.0"
		b.Session.Attributes = json.RawMessage(`{"sdkVersion":"1.0.0"}`)

		injectSDKVersion(&b)

		if got := atributosObj(t, b.Session.Attributes); got["sdkVersion"] != "1.3.0" {
			t.Fatalf("valor do envelope não prevaleceu: %s", b.Session.Attributes)
		}
	})

	t.Run("attributes não-objeto ficam intactos", func(t *testing.T) {
		b := validBatch()
		b.SDKVersion = "1.3.0"
		b.Session.Attributes = json.RawMessage(`[1,2]`)

		injectSDKVersion(&b)

		if string(b.Session.Attributes) != "[1,2]" {
			t.Fatalf("não-objeto foi sobrescrito: %s", b.Session.Attributes)
		}
	})
}

func TestNormalizeBatchMantemComportamentoDeRelease(t *testing.T) {
	b := validBatch()
	b.SDKVersion = "1.3.0"
	b.Session.Attributes = json.RawMessage(`{"release":"2.1.0"}`)
	b.Events[0].Release = ""

	normalizeBatch(&b, time.Now())

	if b.Events[0].Release != "2.1.0" {
		t.Fatalf("release do evento não propagada de session.attributes: %q", b.Events[0].Release)
	}
	got := atributosObj(t, b.Session.Attributes)
	if got["release"] != "2.1.0" || got["sdkVersion"] != "1.3.0" {
		t.Fatalf("atributos incorretos: %s", b.Session.Attributes)
	}
}
