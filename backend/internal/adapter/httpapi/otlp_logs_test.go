package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
)

func logBody(t *testing.T) []byte {
	t.Helper()
	ld := plog.NewLogs()
	lr := ld.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	lr.SetTimestamp(pcommon.NewTimestampFromTime(time.Now().UTC()))
	lr.SetSeverityText("ERROR")
	lr.Body().SetStr("falhou")
	b, err := plogotlp.NewExportRequestFromLogs(ld).MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPostLogs(t *testing.T) {
	f := newOTLPFixture(t, OTLPOptions{})
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(logBody(t)))
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("X-Api-Key", f.server)
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if len(f.logStream.batches) != 1 || f.logStream.batches[0].App != "felpma-api" {
		t.Fatalf("batches = %+v", f.logStream.batches)
	}
}

func TestPostLogsChaveDeBrowser(t *testing.T) {
	f := newOTLPFixture(t, OTLPOptions{})
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(logBody(t)))
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("X-Api-Key", f.brows)
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rr.Code)
	}
}
