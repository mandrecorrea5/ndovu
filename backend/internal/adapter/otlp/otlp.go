// Package otlp traduz o protocolo OTLP/HTTP de traces para o domínio.
//
// A decodificação fica com o pdata do OpenTelemetry Collector (protobuf e
// JSON), para não mantermos os .proto à mão. O resto do Ndovu só enxerga
// domain.Span — nenhum tipo do pdata sai deste pacote.
package otlp

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// Encoding é o formato do corpo, decidido pelo Content-Type.
type Encoding int

const (
	// Protobuf é o default dos exporters OTLP/HTTP.
	Protobuf Encoding = iota
	// JSON é o OTLP/JSON (ids em hex, enums numéricos).
	JSON
)

// EncodingFor resolve o Content-Type. ok=false para tipos que a spec não
// prevê — o handler responde 415.
func EncodingFor(contentType string) (Encoding, bool) {
	mediaType, _, _ := strings.Cut(contentType, ";")
	switch strings.TrimSpace(strings.ToLower(mediaType)) {
	case "application/x-protobuf", "application/protobuf":
		return Protobuf, true
	case "application/json":
		return JSON, true
	}
	return 0, false
}

// ContentType é o Content-Type da resposta no mesmo formato do pedido.
func (e Encoding) ContentType() string {
	if e == JSON {
		return "application/json"
	}
	return "application/x-protobuf"
}

// ErrInvalidPayload indica corpo que não é um ExportTraceServiceRequest. É
// erro permanente: a spec manda responder 400 e o exporter não reenvia.
var ErrInvalidPayload = errors.New("payload OTLP inválido")

// Decode lê um ExportTraceServiceRequest.
func Decode(body []byte, enc Encoding) (ptrace.Traces, error) {
	req := ptraceotlp.NewExportRequest()
	var err error
	if enc == JSON {
		err = req.UnmarshalJSON(body)
	} else {
		err = req.UnmarshalProto(body)
	}
	if err != nil {
		return ptrace.Traces{}, fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}
	return req.Traces(), nil
}

// EncodeResponse monta o ExportTraceServiceResponse. rejected > 0 vira
// partial_success, que o exporter registra mas não reenvia.
func EncodeResponse(enc Encoding, rejected int, message string) ([]byte, error) {
	resp := ptraceotlp.NewExportResponse()
	if rejected > 0 {
		resp.PartialSuccess().SetRejectedSpans(int64(rejected))
		resp.PartialSuccess().SetErrorMessage(message)
	}
	if enc == JSON {
		return resp.MarshalJSON()
	}
	return resp.MarshalProto()
}

// Limites por span. Protegem o armazenamento de instrumentação que despeja
// payloads inteiros em atributos; o excedente é cortado, não rejeitado.
const (
	maxAttrValue  = 4096
	maxAttrKey    = 256
	maxAttrs      = 128
	maxSpanEvents = 128
	maxSpanLinks  = 32
	maxNameLen    = 1024
	// spanBudget é o teto de bytes de atributos (do span, dos eventos e dos
	// links somados) por span. Garante que um span sozinho sempre caiba numa
	// mensagem do stream (1 MB no NATS); o excedente vira valor vazio e o
	// span ganha ndovu.truncated=true.
	spanBudget = 128 << 10
)

// TruncatedAttr marca o span que teve atributos cortados pelo orçamento.
const TruncatedAttr = "ndovu.truncated"

// budget conta os bytes de atributos que ainda cabem no span.
type budget struct {
	left      int
	truncated bool
}

func (b *budget) take(k, v string) (string, string) {
	k = truncate(k, maxAttrKey)
	v = truncate(v, maxAttrValue)
	if b.left < len(k)+len(v) {
		b.truncated = true
		return k, ""
	}
	b.left -= len(k) + len(v)
	return k, v
}

// Window é a faixa de start_time aceita. Fora dela o span é rejeitado: o
// ClickHouse particiona por dia, e um relógio errado no cliente (start em
// 1970) criaria partições que a retenção nunca alcança.
type Window struct {
	MaxAge    time.Duration
	MaxFuture time.Duration
}

// Result é a conversão de um pedido: spans válidos e quantos foram recusados.
type Result struct {
	Spans    []domain.Span
	Rejected int
	// Reason resume a primeira causa de recusa (vai no partial_success).
	Reason string
}

// ToSpans achata o pedido em domain.Span. app vem da chave de API: o
// service.name do resource vira coluna própria, mas nunca escolhe o app.
func ToSpans(td ptrace.Traces, app string, now time.Time, win Window) Result {
	var res Result
	reject := func(reason string) {
		res.Rejected++
		if res.Reason == "" {
			res.Reason = reason
		}
	}
	oldest, newest := now.Add(-win.MaxAge), now.Add(win.MaxFuture)

	for _, rs := range td.ResourceSpans().All() {
		resAttrs := flatten(rs.Resource().Attributes(), &budget{left: spanBudget})
		service := resAttrs["service.name"]
		if service == "" {
			service = "unknown_service"
		}
		for _, ss := range rs.ScopeSpans().All() {
			scope := ss.Scope().Name()
			for _, sp := range ss.Spans().All() {
				if sp.TraceID().IsEmpty() || sp.SpanID().IsEmpty() {
					reject("traceId e spanId são obrigatórios")
					continue
				}
				start := sp.StartTimestamp().AsTime().UTC()
				if start.Before(oldest) || start.After(newest) {
					reject("startTimeUnixNano fora da janela aceita")
					continue
				}
				res.Spans = append(res.Spans, toSpan(sp, app, service, scope, resAttrs, start, now))
			}
		}
	}
	return res
}

func toSpan(sp ptrace.Span, app, service, scope string, resAttrs map[string]string, start, now time.Time) domain.Span {
	// O resource é do processo inteiro e se repete em todo span: fica fora
	// do orçamento (é cortado só pelos limites por valor).
	b := &budget{left: spanBudget}
	attrs := flatten(sp.Attributes(), b)
	duration := sp.EndTimestamp().AsTime().Sub(sp.StartTimestamp().AsTime())
	if duration < 0 {
		duration = 0
	}
	userID := attrs["enduser.id"]
	if userID == "" {
		userID = resAttrs["enduser.id"]
	}

	s := domain.Span{
		App:           app,
		TraceID:       sp.TraceID().String(),
		SpanID:        sp.SpanID().String(),
		TraceState:    sp.TraceState().AsRaw(),
		Name:          truncate(sp.Name(), maxNameLen),
		Kind:          kindName(sp.Kind()),
		ServiceName:   service,
		ScopeName:     scope,
		UserID:        userID,
		StartTime:     start,
		DurationNs:    duration.Nanoseconds(),
		StatusCode:    statusName(sp.Status().Code()),
		StatusMessage: truncate(sp.Status().Message(), maxAttrValue),
		ResourceAttrs: resAttrs,
		Attrs:         attrs,
		ReceivedAt:    now,
	}
	if !sp.ParentSpanID().IsEmpty() {
		s.ParentSpanID = sp.ParentSpanID().String()
	}
	for i, ev := range sp.Events().All() {
		if i >= maxSpanEvents {
			break
		}
		s.Events = append(s.Events, domain.SpanEvent{
			Time:  ev.Timestamp().AsTime().UTC(),
			Name:  truncate(ev.Name(), maxNameLen),
			Attrs: flatten(ev.Attributes(), b),
		})
	}
	for i, l := range sp.Links().All() {
		if i >= maxSpanLinks {
			break
		}
		s.Links = append(s.Links, domain.SpanLink{
			TraceID: l.TraceID().String(),
			SpanID:  l.SpanID().String(),
			Attrs:   flatten(l.Attributes(), b),
		})
	}
	if b.truncated {
		if s.Attrs == nil {
			s.Attrs = map[string]string{}
		}
		s.Attrs[TruncatedAttr] = "true"
	}
	return s
}

// flatten converte atributos OTel em mapa de strings. Valores não-string
// (int, bool, array, map) vão na forma textual do pdata — arrays e mapas em
// JSON —, suficiente para exibir e filtrar por igualdade.
func flatten(m pcommon.Map, b *budget) map[string]string {
	if m.Len() == 0 {
		return nil
	}
	out := make(map[string]string, min(m.Len(), maxAttrs))
	for k, v := range m.All() {
		if len(out) >= maxAttrs {
			b.truncated = true
			break
		}
		k, val := b.take(k, v.AsString())
		out[k] = val
	}
	return out
}

func kindName(k ptrace.SpanKind) string {
	switch k {
	case ptrace.SpanKindInternal:
		return "internal"
	case ptrace.SpanKindServer:
		return "server"
	case ptrace.SpanKindClient:
		return "client"
	case ptrace.SpanKindProducer:
		return "producer"
	case ptrace.SpanKindConsumer:
		return "consumer"
	}
	return "unspecified"
}

func statusName(c ptrace.StatusCode) string {
	switch c {
	case ptrace.StatusCodeOk:
		return domain.SpanStatusOK
	case ptrace.StatusCodeError:
		return domain.SpanStatusError
	}
	return domain.SpanStatusUnset
}

// truncate corta em bytes sem partir um caractere UTF-8 no meio.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && n < len(s) && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}

// EncodeStatus monta o corpo das respostas 4xx/5xx: a spec OTLP/HTTP pede um
// google.rpc.Status no mesmo formato do pedido. Exporters usam o message no
// log de erro, então ele precisa dizer o que corrigir.
func EncodeStatus(enc Encoding, code codes.Code, message string) ([]byte, error) {
	st := &status.Status{Code: int32(code), Message: message} //nolint:gosec // codes.Code cabe em int32
	if enc == JSON {
		return protojson.Marshal(st)
	}
	return proto.Marshal(st)
}
