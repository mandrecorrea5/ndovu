package otlp

import (
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
)

func logRequest(t *testing.T) []byte {
	t.Helper()
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", "elama-api")
	rl.Resource().Attributes().PutStr("enduser.id", "user-1")
	sl := rl.ScopeLogs().AppendEmpty()
	sl.Scope().SetName("http")
	lr := sl.LogRecords().AppendEmpty()
	lr.SetTimestamp(pcommon.NewTimestampFromTime(time.Now().UTC()))
	lr.SetSeverityText("INFO")
	lr.SetSeverityNumber(plog.SeverityNumberInfo)
	lr.Body().SetStr("GET /healthz 200")
	lr.Attributes().PutStr("http.method", "GET")
	lr.Attributes().PutStr("authorization", "Bearer segredo")
	lr.SetTraceID(pcommon.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	lr.SetSpanID(pcommon.SpanID{1, 2, 3, 4, 5, 6, 7, 8})
	req := plogotlp.NewExportRequestFromLogs(ld)
	b, err := req.MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestToLogs(t *testing.T) {
	body := logRequest(t)
	ld, err := DecodeLogs(body, Protobuf)
	if err != nil {
		t.Fatal(err)
	}
	res := ToLogs(ld, "elama-api", time.Now().UTC(), Window{MaxAge: time.Hour, MaxFuture: time.Hour})
	if res.Rejected != 0 || len(res.Logs) != 1 {
		t.Fatalf("res = %+v", res)
	}
	l := res.Logs[0]
	if l.App != "elama-api" || l.ServiceName != "elama-api" || l.Body == "" {
		t.Fatalf("log = %+v", l)
	}
	if l.SeverityText != "info" || l.SeverityNumber == "" {
		t.Fatalf("severidade = %q/%q", l.SeverityText, l.SeverityNumber)
	}
	if l.UserID != "user-1" || l.TraceID == "" || l.SpanID == "" {
		t.Fatalf("correlação = %+v", l)
	}
}

func TestToLogsRejeitaForaDaJanela(t *testing.T) {
	ld := plog.NewLogs()
	lr := ld.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	lr.SetTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-48 * time.Hour)))
	res := ToLogs(ld, "app", time.Now().UTC(), Window{MaxAge: time.Hour, MaxFuture: time.Hour})
	if res.Rejected != 1 || len(res.Logs) != 0 {
		t.Fatalf("res = %+v", res)
	}
}
