package usecase

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// SnapshotService orquestra upload e leitura de snapshots. Comprime com gzip
// antes de gravar (HTML tem alta razão de compressão — 5-10x menor) e
// descomprime na leitura.
type SnapshotService struct {
	meta   domain.SnapshotMetaStore
	blob   domain.SnapshotBlobStore
	logger *slog.Logger
}

// NewSnapshotService cria o serviço. meta/blob são opcionais — se qualquer um
// for nil, o serviço fica em modo "desligado" (retorna erro no Save/Get).
func NewSnapshotService(meta domain.SnapshotMetaStore, blob domain.SnapshotBlobStore, logger *slog.Logger) *SnapshotService {
	return &SnapshotService{meta: meta, blob: blob, logger: logger}
}

// Enabled indica se o serviço pode operar (deps presentes).
func (s *SnapshotService) Enabled() bool { return s.meta != nil && s.blob != nil }

// SnapshotInput é o payload de upload vindo do SDK.
type SnapshotInput struct {
	EventID   string
	SessionID string
	App       string
	HTML      string
	URL       string
	ViewportW int
	ViewportH int
	TakenAt   time.Time
}

// Save comprime e grava. A metadata vai pro Postgres (idempotente por
// event_id), o payload vai pro blob storage sob uma key gerada.
// Limite prático: 2 MB de HTML gzip'ado (~10-20 MB descomprimidos).
func (s *SnapshotService) Save(ctx context.Context, in SnapshotInput) (domain.SessionSnapshot, error) {
	if !s.Enabled() {
		return domain.SessionSnapshot{}, fmt.Errorf("snapshot service desligado (sem blob storage)")
	}
	if in.EventID == "" || in.SessionID == "" || in.App == "" {
		return domain.SessionSnapshot{}, domain.NewValidationError("eventId, sessionId e app são obrigatórios")
	}
	if in.HTML == "" {
		return domain.SessionSnapshot{}, domain.NewValidationError("html vazio")
	}
	if len(in.HTML) > 20*1024*1024 {
		return domain.SessionSnapshot{}, domain.NewValidationError("html acima do limite de 20MB")
	}
	if in.TakenAt.IsZero() {
		in.TakenAt = time.Now().UTC()
	}

	compressed, err := gzipBytes([]byte(in.HTML))
	if err != nil {
		return domain.SessionSnapshot{}, fmt.Errorf("gzip: %w", err)
	}

	key := objectKey(in.EventID, in.TakenAt)
	if err := s.blob.PutSnapshot(ctx, key, compressed); err != nil {
		return domain.SessionSnapshot{}, err
	}

	meta := domain.SessionSnapshot{
		EventID:   in.EventID,
		SessionID: in.SessionID,
		App:       in.App,
		ObjectKey: key,
		SizeBytes: len(compressed),
		ViewportW: in.ViewportW,
		ViewportH: in.ViewportH,
		URL:       in.URL,
		TakenAt:   in.TakenAt.UTC(),
	}
	if err := s.meta.CreateSnapshotMeta(ctx, meta); err != nil {
		return domain.SessionSnapshot{}, err
	}
	s.logger.InfoContext(ctx, "snapshot armazenado",
		"event_id", in.EventID, "app", in.App,
		"gzip_bytes", len(compressed), "raw_bytes", len(in.HTML))
	return meta, nil
}

// Get devolve a metadata e o HTML descompactado. Erros de metadata (404) são
// propagados; erros de blob (S3 fora) idem — o handler responde 500.
func (s *SnapshotService) Get(ctx context.Context, eventID string) (domain.SessionSnapshot, []byte, error) {
	if !s.Enabled() {
		return domain.SessionSnapshot{}, nil, fmt.Errorf("snapshot service desligado")
	}
	meta, err := s.meta.GetSnapshotMetaByEvent(ctx, eventID)
	if err != nil {
		return domain.SessionSnapshot{}, nil, err
	}
	blob, err := s.blob.GetSnapshot(ctx, meta.ObjectKey)
	if err != nil {
		return domain.SessionSnapshot{}, nil, err
	}
	html, err := gunzipBytes(blob)
	if err != nil {
		return domain.SessionSnapshot{}, nil, fmt.Errorf("gunzip: %w", err)
	}
	return meta, html, nil
}

// objectKey gera um caminho previsível para o blob: YYYY/MM/DD/<eventId>-<rand>.html.gz.
// O <rand> evita colisões improváveis se o mesmo eventId fosse retriado com
// TakenAt em segundos diferentes (não deveria acontecer, mas é barato).
func objectKey(eventID string, takenAt time.Time) string {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("%04d/%02d/%02d/%s-%s.html.gz",
		takenAt.Year(), takenAt.Month(), takenAt.Day(),
		eventID, hex.EncodeToString(buf))
}

func gzipBytes(in []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(in); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gunzipBytes(in []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(in))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}
