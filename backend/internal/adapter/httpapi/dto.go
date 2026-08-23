// Package httpapi é o adapter HTTP: DTOs do contrato v1, handlers e middleware.
package httpapi

import (
	"encoding/json"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// ingestRequest é o envelope do contrato de ingestão v1 (ver docs/CONTRACT.md).
type ingestRequest struct {
	App        string     `json:"app"`
	SDKVersion string     `json:"sdkVersion"`
	Session    sessionDTO `json:"session"`
	Events     []eventDTO `json:"events"`
}

type sessionDTO struct {
	SessionID  string          `json:"sessionId"`
	UserID     string          `json:"userId"`
	UserAgent  string          `json:"userAgent"`
	Attributes json.RawMessage `json:"attributes"`
}

type eventDTO struct {
	EventID    string          `json:"eventId"`
	Type       string          `json:"type"`
	Name       string          `json:"name"`
	Feature    string          `json:"feature"`
	Screen     string          `json:"screen"`
	Timestamp  time.Time       `json:"timestamp"`
	DurationMs *int            `json:"durationMs"`
	UserID     string          `json:"userId"`
	Release    string          `json:"release"` // opcional; se ausente, extraímos de session.attributes.release
	HTTP       *httpDTO        `json:"http"`
	Error      *errorDTO       `json:"error"`
	Metadata   json.RawMessage `json:"metadata"`
	Trace      *traceDTO       `json:"trace"` // W3C traceparent decomposto (Sprint D)
}

type traceDTO struct {
	TraceID      string `json:"traceId"`
	SpanID       string `json:"spanId"`
	ParentSpanID string `json:"parentSpanId"`
}

type httpDTO struct {
	Method       string          `json:"method"`
	URL          string          `json:"url"`
	StatusCode   *int            `json:"statusCode"`
	RequestBody  json.RawMessage `json:"requestBody"`
	ResponseBody json.RawMessage `json:"responseBody"`
}

type errorDTO struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Body    json.RawMessage `json:"body"`
}

// toDomain converte o DTO validável em entidades de domínio.
func (r ingestRequest) toDomain() domain.IngestBatch {
	batch := domain.IngestBatch{
		Session: domain.Session{
			SessionID:  r.Session.SessionID,
			UserID:     r.Session.UserID,
			App:        r.App,
			UserAgent:  r.Session.UserAgent,
			Attributes: r.Session.Attributes,
		},
	}
	for _, e := range r.Events {
		ev := domain.TraceEvent{
			ID:         e.EventID,
			UserID:     e.UserID,
			Release:    e.Release,
			Type:       domain.EventType(e.Type),
			Name:       e.Name,
			Feature:    e.Feature,
			Screen:     e.Screen,
			DurationMs: e.DurationMs,
			Metadata:   e.Metadata,
			OccurredAt: e.Timestamp,
		}
		if e.Trace != nil && (e.Trace.TraceID != "" || e.Trace.SpanID != "") {
			ev.Trace = &domain.TraceContext{
				TraceID:      e.Trace.TraceID,
				SpanID:       e.Trace.SpanID,
				ParentSpanID: e.Trace.ParentSpanID,
			}
		}
		if e.HTTP != nil {
			ev.HTTP = &domain.HTTPInfo{
				Method:       e.HTTP.Method,
				URL:          e.HTTP.URL,
				StatusCode:   e.HTTP.StatusCode,
				RequestBody:  e.HTTP.RequestBody,
				ResponseBody: e.HTTP.ResponseBody,
			}
		}
		if e.Error != nil {
			ev.Error = &domain.ErrorInfo{Code: e.Error.Code, Message: e.Error.Message, Body: e.Error.Body}
		}
		batch.Events = append(batch.Events, ev)
	}
	return batch
}

// errorResponse é o formato único de erro da API.
type errorResponse struct {
	Error   string   `json:"error"`
	Details []string `json:"details,omitempty"`
}
