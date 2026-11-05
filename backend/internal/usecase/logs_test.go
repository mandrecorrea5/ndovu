package usecase

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

type captureLogStream struct{ batch domain.LogBatch }

func (c *captureLogStream) PublishLogs(_ context.Context, b domain.LogBatch) error {
	c.batch = b
	return nil
}

func TestLogIngestRedigeAtributos(t *testing.T) {
	stream := &captureLogStream{}
	svc := NewLogIngestService(stream, slog.New(slog.NewTextHandler(io.Discard, nil)))
	err := svc.Ingest(context.Background(), "app", []domain.LogRecord{{
		Body:  "ok",
		Attrs: map[string]string{"authorization": "Bearer x", "http.method": "GET"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if stream.batch.Logs[0].Attrs["authorization"] != RedactedValue {
		t.Fatalf("authorization = %q", stream.batch.Logs[0].Attrs["authorization"])
	}
	if stream.batch.Logs[0].Attrs["http.method"] != "GET" {
		t.Fatal("atributo inocente foi alterado")
	}
}

type failingLogWriter struct{}

func (failingLogWriter) SaveLogs(context.Context, []domain.LogBatch) (int, error) {
	return 0, errors.New("clickhouse fora")
}

func TestLogWriteServiceMandaParaDLQ(t *testing.T) {
	var dlq domain.LogBatch
	svc := NewLogWriteService(failingLogWriter{}, func(_ context.Context, b domain.LogBatch, _ string) error {
		dlq = b
		return nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	err := svc.HandleBatches(context.Background(), []domain.LogBatch{{App: "app", Logs: []domain.LogRecord{{Body: "x"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if dlq.App != "app" {
		t.Fatalf("dlq = %+v", dlq)
	}
}

func TestQueryLogsSemReaderDevolveVazio(t *testing.T) {
	svc := NewQueryService(nil)
	logs, err := svc.Logs(context.Background(), domain.LogFilter{})
	if err != nil || logs == nil || len(logs) != 0 {
		t.Fatalf("logs=%v err=%v", logs, err)
	}
}
