package domain

import "testing"

func TestEventType_Valid(t *testing.T) {
	valid := []EventType{EventPageView, EventAction, EventHTTPRequest, EventError, EventCustom}
	for _, tp := range valid {
		if !tp.Valid() {
			t.Errorf("tipo %q deveria ser válido", tp)
		}
	}
	invalid := []EventType{"", "click", "log", "trace", "pageview" /* sem underline */}
	for _, tp := range invalid {
		if tp.Valid() {
			t.Errorf("tipo %q não deveria ser válido", tp)
		}
	}
}

func TestTraceEvent_TraceIDsSemTraceContextRetornaVazio(t *testing.T) {
	// SDK pode omitir trace quando não integrado ao OTel; TraceIDs deve
	// devolver strings vazias sem panic.
	e := TraceEvent{ID: "x"}
	trace, span, parent := e.TraceIDs()
	if trace != "" || span != "" || parent != "" {
		t.Errorf("esperava strings vazias, veio (%q,%q,%q)", trace, span, parent)
	}
}

func TestTraceEvent_TraceIDsPropagaCampos(t *testing.T) {
	e := TraceEvent{
		Trace: &TraceContext{
			TraceID:      "abc123",
			SpanID:       "s1",
			ParentSpanID: "p1",
		},
	}
	trace, span, parent := e.TraceIDs()
	if trace != "abc123" {
		t.Errorf("traceId errado: %q", trace)
	}
	if span != "s1" {
		t.Errorf("spanId errado: %q", span)
	}
	if parent != "p1" {
		t.Errorf("parentSpanId errado: %q", parent)
	}
}

func TestTraceEvent_HasError(t *testing.T) {
	cases := []struct {
		name  string
		event TraceEvent
		want  bool
	}{
		{"sem error", TraceEvent{}, false},
		{"error com ponteiro mas ambos campos vazios (não conta)", TraceEvent{Error: &ErrorInfo{}}, false},
		{"error com code", TraceEvent{Error: &ErrorInfo{Code: "AUTH_401"}}, true},
		{"error com message", TraceEvent{Error: &ErrorInfo{Message: "algo falhou"}}, true},
		{"error com code + message", TraceEvent{Error: &ErrorInfo{Code: "X", Message: "y"}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.event.HasError(); got != c.want {
				t.Errorf("esperado %v, veio %v", c.want, got)
			}
		})
	}
}
