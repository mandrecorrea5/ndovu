package domain

import (
	"context"
	"time"
)

// Span é um span OpenTelemetry recebido por OTLP, já achatado para o
// armazenamento: atributos viram mapa de strings (valor não-string vai
// serializado em JSON) e o app vem da chave de API, nunca do payload.
//
// Não reaproveita TraceEvent de propósito: span tem início/fim, kind, status,
// eventos e links — forçar no contrato de evento perderia informação.
type Span struct {
	App           string            `json:"app"`
	TraceID       string            `json:"traceId"`
	SpanID        string            `json:"spanId"`
	ParentSpanID  string            `json:"parentSpanId,omitempty"`
	TraceState    string            `json:"traceState,omitempty"`
	Name          string            `json:"name"`
	Kind          string            `json:"kind"` // internal | server | client | producer | consumer | unspecified
	ServiceName   string            `json:"serviceName"`
	ScopeName     string            `json:"scopeName,omitempty"`
	UserID        string            `json:"userId,omitempty"` // enduser.id — chave do esquecimento LGPD
	StartTime     time.Time         `json:"startTime"`
	DurationNs    int64             `json:"durationNs"`
	StatusCode    string            `json:"statusCode"` // unset | ok | error
	StatusMessage string            `json:"statusMessage,omitempty"`
	ResourceAttrs map[string]string `json:"resourceAttributes,omitempty"`
	Attrs         map[string]string `json:"attributes,omitempty"`
	Events        []SpanEvent       `json:"events,omitempty"`
	Links         []SpanLink        `json:"links,omitempty"`
	ReceivedAt    time.Time         `json:"receivedAt"`
}

// SpanEvent é um evento pontual dentro do span (ex.: "exception").
type SpanEvent struct {
	Time  time.Time         `json:"time"`
	Name  string            `json:"name"`
	Attrs map[string]string `json:"attributes,omitempty"`
}

// SpanLink aponta para um span de outro trace (ex.: consumo em lote).
type SpanLink struct {
	TraceID string            `json:"traceId"`
	SpanID  string            `json:"spanId"`
	Attrs   map[string]string `json:"attributes,omitempty"`
}

// Status de span normalizados (minúsculos, como guardados).
const (
	SpanStatusUnset = "unset"
	SpanStatusOK    = "ok"
	SpanStatusError = "error"
)

// HasError diz se o span representa falha: status ERROR ou um evento
// "exception" (convenção semântica — há instrumentações que registram a
// exceção sem marcar o status).
func (s Span) HasError() bool {
	if s.StatusCode == SpanStatusError {
		return true
	}
	for _, e := range s.Events {
		if e.Name == "exception" {
			return true
		}
	}
	return false
}

// SpanBatch é a unidade transportada no stream: spans de um app vindos de
// uma mesma requisição OTLP (ou um pedaço dela, se for grande).
type SpanBatch struct {
	App   string `json:"app"`
	Spans []Span `json:"spans"`
}

// SpanStream é o port de publicação dos spans recebidos por OTLP.
type SpanStream interface {
	PublishSpans(ctx context.Context, batch SpanBatch) error
}

// SpanWriter persiste spans no armazenamento analítico.
type SpanWriter interface {
	SaveSpans(ctx context.Context, batches []SpanBatch) (int, error)
}

// SpanReader é o lado de leitura dos spans.
type SpanReader interface {
	// TraceSpans devolve os spans de um trace, restritos aos apps do scope
	// (scope vazio = sem restrição, super-admin).
	TraceSpans(ctx context.Context, traceID string, scope []string) ([]Span, error)
	// CountSpansByApp conta spans persistidos por app desde `since` — usado
	// para reconstruir o contador da cota diária quando o writer reinicia.
	CountSpansByApp(ctx context.Context, since time.Time) (map[string]int, error)
}
