package domain

import (
	"context"
	"time"
)

// LogRecord é um registro OTLP já achatado para armazenamento. O app vem da
// chave de API; service.name e scope viram colunas próprias.
type LogRecord struct {
	App            string            `json:"app"`
	ServiceName    string            `json:"serviceName"`
	ScopeName      string            `json:"scopeName,omitempty"`
	SeverityText   string            `json:"severityText"`
	SeverityNumber string            `json:"severityNumber,omitempty"`
	Body           string            `json:"body"`
	TraceID        string            `json:"traceId,omitempty"`
	SpanID         string            `json:"spanId,omitempty"`
	UserID         string            `json:"userId,omitempty"`
	Time           time.Time         `json:"time"`
	ObservedTime   time.Time         `json:"observedTime,omitempty"`
	Attrs          map[string]string `json:"attributes,omitempty"`
	ResourceAttrs  map[string]string `json:"resourceAttributes,omitempty"`
	ReceivedAt     time.Time         `json:"receivedAt"`
}

// LogBatch é a unidade transportada no stream.
type LogBatch struct {
	App  string      `json:"app"`
	Logs []LogRecord `json:"logs"`
}

// LogStream é o port de publicação dos logs recebidos por OTLP.
type LogStream interface {
	PublishLogs(ctx context.Context, batch LogBatch) error
}

// LogWriter persiste logs no armazenamento analítico.
type LogWriter interface {
	SaveLogs(ctx context.Context, batches []LogBatch) (int, error)
}

// LogFilter são os filtros da busca de logs no dashboard.
type LogFilter struct {
	App      string
	Service  string
	Severity string
	Search   string
	From     *time.Time
	To       *time.Time
	Scope    []string // apps que o requester pode ver; vazio = sem restrição
	Limit    int
}

// LogReader é o lado de leitura dos logs.
type LogReader interface {
	FindLogs(ctx context.Context, f LogFilter) ([]LogRecord, error)
}
