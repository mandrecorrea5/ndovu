package otlp

import (
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

var (
	testNow    = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	testWindow = Window{MaxAge: 30 * 24 * time.Hour, MaxFuture: time.Hour}
	traceID    = pcommon.TraceID{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36}
)

// sampleTraces monta um pedido com um span SERVER e um CLIENT filho.
func sampleTraces() ptrace.Traces {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "felpma-api")
	rs.Resource().Attributes().PutStr("deployment.environment", "prod")
	ss := rs.ScopeSpans().AppendEmpty()
	ss.Scope().SetName("@opentelemetry/instrumentation-http")

	start := testNow.Add(-time.Minute)
	server := ss.Spans().AppendEmpty()
	server.SetTraceID(traceID)
	server.SetSpanID(pcommon.SpanID{1, 2, 3, 4, 5, 6, 7, 8})
	server.SetParentSpanID(pcommon.SpanID{9, 9, 9, 9, 9, 9, 9, 9})
	server.SetName("POST /contato")
	server.SetKind(ptrace.SpanKindServer)
	server.SetStartTimestamp(pcommon.NewTimestampFromTime(start))
	server.SetEndTimestamp(pcommon.NewTimestampFromTime(start.Add(120 * time.Millisecond)))
	server.Status().SetCode(ptrace.StatusCodeError)
	server.Status().SetMessage("boom")
	server.Attributes().PutStr("http.route", "/contato")
	server.Attributes().PutInt("http.response.status_code", 500)
	server.Attributes().PutStr("enduser.id", "u-42")
	ev := server.Events().AppendEmpty()
	ev.SetName("exception")
	ev.SetTimestamp(pcommon.NewTimestampFromTime(start.Add(100 * time.Millisecond)))
	ev.Attributes().PutStr("exception.type", "TypeError")
	link := server.Links().AppendEmpty()
	link.SetTraceID(pcommon.TraceID{0xaa, 1})
	link.SetSpanID(pcommon.SpanID{0xbb, 1})

	client := ss.Spans().AppendEmpty()
	client.SetTraceID(traceID)
	client.SetSpanID(pcommon.SpanID{2, 2, 2, 2, 2, 2, 2, 2})
	client.SetParentSpanID(server.SpanID())
	client.SetName("SELECT contatos")
	client.SetKind(ptrace.SpanKindClient)
	client.SetStartTimestamp(pcommon.NewTimestampFromTime(start.Add(10 * time.Millisecond)))
	client.SetEndTimestamp(pcommon.NewTimestampFromTime(start.Add(40 * time.Millisecond)))
	return td
}

func TestDecodeProtobufEJSON(t *testing.T) {
	req := ptraceotlp.NewExportRequestFromTraces(sampleTraces())
	pb, err := req.MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	js, err := req.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		body []byte
		enc  Encoding
	}{"protobuf": {pb, Protobuf}, "json": {js, JSON}} {
		td, err := Decode(tc.body, tc.enc)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if td.SpanCount() != 2 {
			t.Fatalf("%s: spans = %d, quero 2", name, td.SpanCount())
		}
	}
	if _, err := Decode([]byte("lixo"), JSON); err == nil {
		t.Fatal("JSON inválido deveria falhar")
	}
}

func TestToSpansAchataOSpan(t *testing.T) {
	res := ToSpans(sampleTraces(), "felpma-api", testNow, testWindow)
	if res.Rejected != 0 || len(res.Spans) != 2 {
		t.Fatalf("rejected=%d spans=%d", res.Rejected, len(res.Spans))
	}
	s := res.Spans[0]
	checks := map[string][2]string{
		"app":      {s.App, "felpma-api"},
		"traceId":  {s.TraceID, "4bf92f3577b34da6a3ce929d0e0e4736"},
		"spanId":   {s.SpanID, "0102030405060708"},
		"parent":   {s.ParentSpanID, "0909090909090909"},
		"kind":     {s.Kind, "server"},
		"service":  {s.ServiceName, "felpma-api"},
		"scope":    {s.ScopeName, "@opentelemetry/instrumentation-http"},
		"status":   {s.StatusCode, domain.SpanStatusError},
		"message":  {s.StatusMessage, "boom"},
		"userId":   {s.UserID, "u-42"},
		"intAttr":  {s.Attrs["http.response.status_code"], "500"},
		"resource": {s.ResourceAttrs["deployment.environment"], "prod"},
		"linkSpan": {s.Links[0].SpanID, "bb01000000000000"},
		"evento":   {s.Events[0].Attrs["exception.type"], "TypeError"},
	}
	for name, c := range checks {
		if c[0] != c[1] {
			t.Errorf("%s = %q, quero %q", name, c[0], c[1])
		}
	}
	if s.DurationNs != (120 * time.Millisecond).Nanoseconds() {
		t.Errorf("duração = %d", s.DurationNs)
	}
	if !s.StartTime.Equal(testNow.Add(-time.Minute)) || !s.ReceivedAt.Equal(testNow) {
		t.Errorf("start=%v received=%v", s.StartTime, s.ReceivedAt)
	}
	if !s.HasError() {
		t.Error("span com status ERROR deveria ter erro")
	}
	if res.Spans[1].Kind != "client" || res.Spans[1].ParentSpanID != s.SpanID {
		t.Errorf("filho: kind=%s parent=%s", res.Spans[1].Kind, res.Spans[1].ParentSpanID)
	}
}

func TestToSpansRecusaIdVazioEForaDaJanela(t *testing.T) {
	td := sampleTraces()
	spans := td.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
	spans.At(0).SetTraceID(pcommon.TraceID{}) // sem traceId
	spans.At(1).SetStartTimestamp(pcommon.NewTimestampFromTime(time.Unix(0, 0)))

	res := ToSpans(td, "felpma-api", testNow, testWindow)
	if len(res.Spans) != 0 || res.Rejected != 2 {
		t.Fatalf("spans=%d rejected=%d, quero 0 e 2", len(res.Spans), res.Rejected)
	}
	if res.Reason == "" {
		t.Error("recusa sem motivo para o partial_success")
	}
}

func TestToSpansServicoDesconhecido(t *testing.T) {
	td := sampleTraces()
	td.ResourceSpans().At(0).Resource().Attributes().Remove("service.name")
	if got := ToSpans(td, "a", testNow, testWindow).Spans[0].ServiceName; got != "unknown_service" {
		t.Fatalf("service = %q", got)
	}
}

func TestOrcamentoDeAtributosPorSpan(t *testing.T) {
	td := sampleTraces()
	sp := td.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	big := strings.Repeat("x", maxAttrValue*2)
	for i := range maxAttrs {
		sp.Attributes().PutStr("attr."+strings.Repeat("k", 3)+string(rune('a'+i%26))+string(rune('a'+i/26)), big)
	}

	s := ToSpans(td, "a", testNow, testWindow).Spans[0]
	total := 0
	for k, v := range s.Attrs {
		if len(v) > maxAttrValue {
			t.Fatalf("%s com %d bytes passou do teto por valor", k, len(v))
		}
		total += len(k) + len(v)
	}
	if total > spanBudget+len(TruncatedAttr)+len("true") {
		t.Fatalf("atributos somam %d bytes, orçamento é %d", total, spanBudget)
	}
	if s.Attrs[TruncatedAttr] != "true" {
		t.Fatal("span cortado sem a marca ndovu.truncated")
	}
}

func TestTruncateNaoPartiUTF8(t *testing.T) {
	if got := truncate("ação", 2); got != "a" {
		t.Fatalf("truncate = %q, quero %q", got, "a")
	}
}

func TestEncodingFor(t *testing.T) {
	for ct, want := range map[string]Encoding{
		"application/x-protobuf":          Protobuf,
		"application/json; charset=utf-8": JSON,
	} {
		if got, ok := EncodingFor(ct); !ok || got != want {
			t.Errorf("%s → %v %v", ct, got, ok)
		}
	}
	if _, ok := EncodingFor("text/plain"); ok {
		t.Error("text/plain não deveria ser aceito")
	}
}

func TestRespostasSeguemASpec(t *testing.T) {
	body, err := EncodeResponse(Protobuf, 3, "fora da janela")
	if err != nil {
		t.Fatal(err)
	}
	resp := ptraceotlp.NewExportResponse()
	if err := resp.UnmarshalProto(body); err != nil {
		t.Fatal(err)
	}
	if resp.PartialSuccess().RejectedSpans() != 3 || resp.PartialSuccess().ErrorMessage() != "fora da janela" {
		t.Fatalf("partial_success = %d %q", resp.PartialSuccess().RejectedSpans(), resp.PartialSuccess().ErrorMessage())
	}

	body, err = EncodeStatus(Protobuf, codes.PermissionDenied, "sem OTLP")
	if err != nil {
		t.Fatal(err)
	}
	var st status.Status
	if err := proto.Unmarshal(body, &st); err != nil || st.GetMessage() != "sem OTLP" || codes.Code(st.GetCode()) != codes.PermissionDenied {
		t.Fatalf("status = %v %v", &st, err)
	}
}
