package ndovu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestFlushSendsContractAndRedactsSensitiveFields(t *testing.T) {
	var mu sync.Mutex
	var received envelope
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/events" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Api-Key"); got != "test-key" {
			t.Errorf("unexpected API key header: %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("unexpected content type: %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client, err := New(Config{Endpoint: server.URL, APIKey: "test-key", App: "orders", FlushInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close(context.Background())

	err = client.Action("order_created", EventOptions{
		Feature: "orders",
		Session: &Session{SessionID: "user-session", Attributes: map[string]any{"secret": "do-not-send"}},
		Metadata: map[string]any{
			"password": "do-not-send",
			"nested":   map[string]any{"authorization": "secret"},
			"typed":    map[string]string{"token": "do-not-send"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}

	if received.App != "orders" || received.SDKVersion != sdkVersion || len(received.Events) != 1 {
		t.Fatalf("unexpected envelope: %+v", received)
	}
	event := received.Events[0]
	if event.Type != "action" || event.Name != "order_created" || event.EventID == "" || event.Timestamp == "" {
		t.Fatalf("unexpected event: %+v", event)
	}
	if event.Metadata["password"] != "***" {
		t.Fatalf("password was not redacted: %#v", event.Metadata)
	}
	nested, ok := event.Metadata["nested"].(map[string]any)
	if !ok || nested["authorization"] != "***" {
		t.Fatalf("nested authorization was not redacted: %#v", event.Metadata)
	}
	typed, ok := event.Metadata["typed"].(map[string]any)
	if !ok || typed["token"] != "***" {
		t.Fatalf("typed map token was not redacted: %#v", event.Metadata)
	}
	if received.Session.SessionID != "user-session" || received.Session.Attributes["secret"] != "***" {
		t.Fatalf("session data was not preserved and redacted: %+v", received.Session)
	}
}

func TestFlushKeepsEventsAfterFailureAndCloseRetries(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client, err := New(Config{Endpoint: server.URL, APIKey: "key", App: "app", FlushInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Custom("event", EventOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := client.Flush(context.Background()); err == nil {
		t.Fatal("expected non-success HTTP status to fail")
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("expected retry on close, got %d requests", requests)
	}
	if err := client.Action("after_close", EventOptions{}); err == nil {
		t.Fatal("expected recording on closed client to fail")
	}
}

func TestFlushSplitsBatchesAndGroupsSessions(t *testing.T) {
	var batchSizes []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got envelope
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		batchSizes = append(batchSizes, len(got.Events))
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	client, err := New(Config{Endpoint: server.URL, APIKey: "key", App: "app", FlushInterval: time.Hour, MaxBatch: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close(context.Background())
	for i := 0; i < 3; i++ {
		if err := client.Action("same_session", EventOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.Action("other_session", EventOptions{Session: &Session{SessionID: "explicit"}}); err != nil {
		t.Fatal(err)
	}
	if err := client.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(batchSizes) != 3 || batchSizes[0] != 2 || batchSizes[1] != 1 || batchSizes[2] != 1 {
		t.Fatalf("unexpected batch sizes: %v", batchSizes)
	}
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	tests := []Config{
		{APIKey: "key", App: "app"},
		{Endpoint: "ftp://localhost", APIKey: "key", App: "app"},
		{Endpoint: "https://example.test?token=secret", APIKey: "key", App: "app"},
	}
	for _, config := range tests {
		if _, err := New(config); err == nil {
			t.Errorf("New(%+v) succeeded; expected validation error", config)
		}
	}
}
