// Package domain contém o núcleo da aplicação: entidades, regras e ports.
// Não importa nada de HTTP, SQL ou frameworks — dependências apontam para cá.
package domain

import (
	"encoding/json"
	"time"
)

// EventType classifica o evento dentro do rastro do usuário.
type EventType string

const (
	EventPageView    EventType = "page_view"
	EventAction      EventType = "action"
	EventHTTPRequest EventType = "http_request"
	EventError       EventType = "error"
	EventCustom      EventType = "custom"
)

// Valid informa se o tipo é reconhecido pelo contrato v1.
func (t EventType) Valid() bool {
	switch t {
	case EventPageView, EventAction, EventHTTPRequest, EventError, EventCustom:
		return true
	}
	return false
}

// HTTPInfo descreve a chamada HTTP associada a um evento.
type HTTPInfo struct {
	Method       string          `json:"method,omitempty"`
	URL          string          `json:"url,omitempty"`
	StatusCode   *int            `json:"statusCode,omitempty"`
	RequestBody  json.RawMessage `json:"requestBody,omitempty"`
	ResponseBody json.RawMessage `json:"responseBody,omitempty"`
}

// ErrorInfo descreve a falha associada a um evento.
type ErrorInfo struct {
	Code    string          `json:"code,omitempty"`
	Message string          `json:"message,omitempty"`
	Body    json.RawMessage `json:"body,omitempty"`
}

// TraceContext identifica o evento dentro de um trace distribuído W3C. Todo
// campo é opcional — o SDK pode omitir se não estiver integrado ao OTel.
type TraceContext struct {
	TraceID      string `json:"traceId,omitempty"`
	SpanID       string `json:"spanId,omitempty"`
	ParentSpanID string `json:"parentSpanId,omitempty"`
}

// TraceEvent é o fato imutável: algo que o usuário fez (ou sofreu) no frontend.
type TraceEvent struct {
	ID         string          `json:"eventId"`
	SessionID  string          `json:"sessionId"`
	UserID     string          `json:"userId,omitempty"`
	App        string          `json:"app"`
	Release    string          `json:"release,omitempty"`
	Type       EventType       `json:"type"`
	Name       string          `json:"name"`
	Feature    string          `json:"feature,omitempty"`
	Screen     string          `json:"screen,omitempty"`
	HTTP       *HTTPInfo       `json:"http,omitempty"`
	Error      *ErrorInfo      `json:"error,omitempty"`
	DurationMs *int            `json:"durationMs,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	Trace      *TraceContext   `json:"trace,omitempty"`
	OccurredAt time.Time       `json:"timestamp"`
	ReceivedAt time.Time       `json:"receivedAt,omitempty"`
}

// TraceIDs devolve os identificadores W3C (ou strings vazias se ausentes).
func (e TraceEvent) TraceIDs() (traceID, spanID, parentSpanID string) {
	if e.Trace == nil {
		return "", "", ""
	}
	return e.Trace.TraceID, e.Trace.SpanID, e.Trace.ParentSpanID
}

// HasError informa se o evento representa uma falha.
func (e TraceEvent) HasError() bool {
	return e.Error != nil && (e.Error.Code != "" || e.Error.Message != "")
}

// Session agrega o rastro de um usuário em um frontend.
type Session struct {
	SessionID   string          `json:"sessionId"`
	UserID      string          `json:"userId,omitempty"`
	App         string          `json:"app"`
	UserAgent   string          `json:"userAgent,omitempty"`
	Attributes  json.RawMessage `json:"attributes,omitempty"`
	StartedAt   time.Time       `json:"startedAt"`
	LastEventAt time.Time       `json:"lastEventAt"`
	EventCount  int             `json:"eventCount"`
	ErrorCount  int             `json:"errorCount"`
}
