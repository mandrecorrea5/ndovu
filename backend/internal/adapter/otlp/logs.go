package otlp

// Decodificação de logs OTLP para o domínio. Mesmo desenho do arquivo de
// traces: o pdata cuida de protobuf/JSON e o resto do Ndovu só vê
// domain.LogRecord.

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// Limites por registro de log. O corpo é cortado, não rejeitado.
const (
	maxLogBody   = 64 << 10
	maxLogAttrs  = 128
	logBudget    = 128 << 10
	logTruncated = "ndovu.truncated"
)

// ErrInvalidLogPayload indica corpo que não é um ExportLogsServiceRequest.
var ErrInvalidLogPayload = fmt.Errorf("%w: logs", ErrInvalidPayload)

// DecodeLogs lê um ExportLogsServiceRequest.
func DecodeLogs(body []byte, enc Encoding) (plog.Logs, error) {
	req := plogotlp.NewExportRequest()
	var err error
	if enc == JSON {
		err = req.UnmarshalJSON(body)
	} else {
		err = req.UnmarshalProto(body)
	}
	if err != nil {
		return plog.Logs{}, fmt.Errorf("%w: %v", ErrInvalidLogPayload, err)
	}
	return req.Logs(), nil
}

// EncodeLogsResponse monta o ExportLogsServiceResponse.
func EncodeLogsResponse(enc Encoding, rejected int, message string) ([]byte, error) {
	resp := plogotlp.NewExportResponse()
	if rejected > 0 {
		resp.PartialSuccess().SetRejectedLogRecords(int64(rejected))
		resp.PartialSuccess().SetErrorMessage(message)
	}
	if enc == JSON {
		return resp.MarshalJSON()
	}
	return resp.MarshalProto()
}

// LogResult é a conversão de um pedido de logs.
type LogResult struct {
	Logs     []domain.LogRecord
	Rejected int
	Reason   string
}

// ToLogs achata o pedido em domain.LogRecord.
func ToLogs(ld plog.Logs, app string, now time.Time, win Window) LogResult {
	var res LogResult
	reject := func(reason string) {
		res.Rejected++
		if res.Reason == "" {
			res.Reason = reason
		}
	}
	oldest, newest := now.Add(-win.MaxAge), now.Add(win.MaxFuture)

	for _, rl := range ld.ResourceLogs().All() {
		resAttrs := flatten(rl.Resource().Attributes(), &budget{left: logBudget})
		service := resAttrs["service.name"]
		if service == "" {
			service = "unknown_service"
		}
		for _, sl := range rl.ScopeLogs().All() {
			scope := sl.Scope().Name()
			for _, lr := range sl.LogRecords().All() {
				ts := lr.Timestamp().AsTime().UTC()
				if ts.IsZero() {
					ts = lr.ObservedTimestamp().AsTime().UTC()
				}
				if ts.IsZero() {
					ts = now
				}
				if ts.Before(oldest) || ts.After(newest) {
					reject("timeUnixNano fora da janela aceita")
					continue
				}
				res.Logs = append(res.Logs, toLogRecord(lr, app, service, scope, resAttrs, ts, now))
			}
		}
	}
	return res
}

func toLogRecord(lr plog.LogRecord, app, service, scope string, resAttrs map[string]string, ts, now time.Time) domain.LogRecord {
	b := &budget{left: logBudget}
	attrs := flatten(lr.Attributes(), b)
	if len(attrs) > maxLogAttrs {
		attrs = nil
	}
	sevText := strings.ToLower(strings.TrimSpace(lr.SeverityText()))
	sevNum := strconv.Itoa(int(lr.SeverityNumber()))
	if sevText == "" && lr.SeverityNumber() != plog.SeverityNumberUnspecified {
		sevText = strings.ToLower(lr.SeverityNumber().String())
	}
	userID := attrs["enduser.id"]
	if userID == "" {
		userID = resAttrs["enduser.id"]
	}
	rec := domain.LogRecord{
		App:            app,
		ServiceName:    service,
		ScopeName:      scope,
		SeverityText:   sevText,
		SeverityNumber: sevNum,
		Body:           truncate(valueString(lr.Body()), maxLogBody),
		TraceID:        lr.TraceID().String(),
		SpanID:         lr.SpanID().String(),
		UserID:         userID,
		Time:           ts,
		ObservedTime:   lr.ObservedTimestamp().AsTime().UTC(),
		Attrs:          attrs,
		ResourceAttrs:  resAttrs,
		ReceivedAt:     now,
	}
	if b.truncated {
		if rec.Attrs == nil {
			rec.Attrs = map[string]string{}
		}
		rec.Attrs[logTruncated] = "true"
	}
	return rec
}

func valueString(v pcommon.Value) string {
	switch v.Type() {
	case pcommon.ValueTypeStr:
		return v.Str()
	case pcommon.ValueTypeInt:
		return strconv.FormatInt(v.Int(), 10)
	case pcommon.ValueTypeDouble:
		return strconv.FormatFloat(v.Double(), 'f', -1, 64)
	case pcommon.ValueTypeBool:
		return strconv.FormatBool(v.Bool())
	case pcommon.ValueTypeBytes:
		return base64.StdEncoding.EncodeToString(v.Bytes().AsRaw())
	case pcommon.ValueTypeMap, pcommon.ValueTypeSlice:
		raw, err := json.Marshal(v.AsRaw())
		if err != nil {
			return ""
		}
		return string(raw)
	default:
		return ""
	}
}
