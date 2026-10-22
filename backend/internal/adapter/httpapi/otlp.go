package httpapi

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"google.golang.org/grpc/codes"

	"github.com/marcoscorrea/ndovu/backend/internal/adapter/otlp"
	"github.com/marcoscorrea/ndovu/backend/internal/domain"
	"github.com/marcoscorrea/ndovu/backend/internal/platform"
	"github.com/marcoscorrea/ndovu/backend/internal/usecase"
)

// OTLPOptions parametriza o receptor OTLP.
type OTLPOptions struct {
	MaxBodyBytes   int64         // corpo como chega
	MaxDecodedSize int64         // corpo depois do gunzip
	MaxSpanAge     time.Duration // start_time mais antigo aceito (= retenção)
}

// NewOTLPRouter monta o receptor OTLP/HTTP — servido num listener próprio,
// atrás do host otlp.<domínio>, com os paths padrão da spec. Assim o cliente
// configura só OTEL_EXPORTER_OTLP_ENDPOINT e o POST /v1/traces daqui nunca
// se confunde com o GET /v1/traces/{traceId} do dashboard.
//
// Sem otelhttp de propósito: o Ndovu pode exportar os próprios traces para
// este receptor, e rastrear o recebimento criaria spans a cada exportação.
func NewOTLPRouter(ingest *usecase.SpanIngestService, keys *usecase.APIKeyService, metrics *platform.Metrics, logger *slog.Logger, opts OTLPOptions) http.Handler {
	// Zero = não configurado (Config montado à mão em teste): defaults da
	// config, nunca um teto literal de 0.
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = 4 << 20
	}
	if opts.MaxDecodedSize <= 0 {
		opts.MaxDecodedSize = 32 << 20
	}
	if opts.MaxSpanAge <= 0 {
		opts.MaxSpanAge = 30 * 24 * time.Hour
	}
	h := &otlpHandler{ingest: ingest, keys: keys, metrics: metrics, logger: logger, opts: opts, now: time.Now}
	r := chi.NewRouter()
	r.Use(recoverer(logger))
	r.Use(requestLogger(logger, metrics))
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Post("/v1/traces", h.PostTraces)
	return r
}

type otlpHandler struct {
	ingest  *usecase.SpanIngestService
	keys    *usecase.APIKeyService
	metrics *platform.Metrics
	logger  *slog.Logger
	opts    OTLPOptions
	now     func() time.Time
}

// PostTraces recebe um ExportTraceServiceRequest. Códigos seguem a spec
// OTLP/HTTP: 429/503 são reenviados pelo exporter; 400/401/403/413/415 não.
func (h *otlpHandler) PostTraces(w http.ResponseWriter, r *http.Request) {
	ctx := platform.WithoutTracing(r.Context())
	enc, ok := otlp.EncodingFor(r.Header.Get("Content-Type"))
	if !ok {
		// Sem formato conhecido, responde em protobuf (o default da spec).
		h.fail(w, otlp.Protobuf, http.StatusUnsupportedMediaType, codes.InvalidArgument,
			"Content-Type deve ser application/x-protobuf ou application/json")
		return
	}

	key, err := h.keys.Validate(ctx, r.Header.Get("X-Api-Key"))
	if err != nil {
		if errors.Is(err, domain.ErrUnauthorized) {
			h.fail(w, enc, http.StatusUnauthorized, codes.Unauthenticated, "x-api-key ausente, inválida ou revogada")
			return
		}
		h.fail(w, enc, http.StatusServiceUnavailable, codes.Unavailable, "validação da chave indisponível")
		return
	}
	if !key.AllowOTLP {
		h.fail(w, enc, http.StatusForbidden, codes.PermissionDenied,
			fmt.Sprintf("o app %q não aceita OTLP — marque-o como app de servidor no Ndovu", key.App))
		return
	}
	if !h.keys.Allow(key) {
		h.count("ndovu_otlp_rate_limited_total", "Exportações OTLP recusadas por rate limit", key.App, 1)
		w.Header().Set("Retry-After", "1")
		h.fail(w, enc, http.StatusTooManyRequests, codes.ResourceExhausted, "rate limit da chave excedido")
		return
	}

	body, status, msg := h.readBody(w, r)
	if status != 0 {
		h.fail(w, enc, status, codes.InvalidArgument, msg)
		return
	}
	td, err := otlp.Decode(body, enc)
	if err != nil {
		h.fail(w, enc, http.StatusBadRequest, codes.InvalidArgument, err.Error())
		return
	}

	res := otlp.ToSpans(td, key.App, h.now().UTC(), otlp.Window{MaxAge: h.opts.MaxSpanAge, MaxFuture: time.Hour})
	if err := h.ingest.Ingest(ctx, key.App, res.Spans); err != nil {
		h.logger.ErrorContext(ctx, "otlp: falha ao enfileirar spans", "app", key.App, "err", err)
		w.Header().Set("Retry-After", "5")
		h.fail(w, enc, http.StatusServiceUnavailable, codes.Unavailable, "buffer de ingestão indisponível — reenvie")
		return
	}
	h.count("ndovu_otlp_spans_received_total", "Spans OTLP aceitos", key.App, len(res.Spans))
	h.count("ndovu_otlp_spans_rejected_total", "Spans OTLP recusados (partial_success)", key.App, res.Rejected)

	out, err := otlp.EncodeResponse(enc, res.Rejected, res.Reason)
	if err != nil {
		h.fail(w, enc, http.StatusInternalServerError, codes.Internal, "erro ao montar a resposta")
		return
	}
	w.Header().Set("Content-Type", enc.ContentType())
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// readBody lê o corpo respeitando dois tetos: o do corpo como chega e o do
// corpo descomprimido. Só o primeiro (MaxBytesReader) não basta — um gzip de
// 1 MB pode virar gigabytes. status != 0 indica recusa.
func (h *otlpHandler) readBody(w http.ResponseWriter, r *http.Request) ([]byte, int, string) {
	var reader io.Reader = http.MaxBytesReader(w, r.Body, h.opts.MaxBodyBytes)
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return nil, http.StatusBadRequest, "corpo gzip inválido"
		}
		defer gz.Close()
		reader = gz
	default:
		return nil, http.StatusUnsupportedMediaType, "Content-Encoding suportado: gzip"
	}

	body, err := io.ReadAll(io.LimitReader(reader, h.opts.MaxDecodedSize+1))
	var maxErr *http.MaxBytesError
	switch {
	case errors.As(err, &maxErr):
		return nil, http.StatusRequestEntityTooLarge, "corpo acima do limite"
	case err != nil:
		return nil, http.StatusBadRequest, "corpo ilegível: " + err.Error()
	case int64(len(body)) > h.opts.MaxDecodedSize:
		return nil, http.StatusRequestEntityTooLarge, "corpo descomprimido acima do limite"
	}
	return body, 0, ""
}

func (h *otlpHandler) fail(w http.ResponseWriter, enc otlp.Encoding, httpStatus int, code codes.Code, message string) {
	body, err := otlp.EncodeStatus(enc, code, message)
	if err != nil {
		http.Error(w, message, httpStatus)
		return
	}
	w.Header().Set("Content-Type", enc.ContentType())
	w.WriteHeader(httpStatus)
	_, _ = w.Write(body)
}

func (h *otlpHandler) count(name, help, app string, n int) {
	if h.metrics == nil || n == 0 {
		return
	}
	h.metrics.Counter(name, help, map[string]string{"app": app}, float64(n))
}
