// Ndovu API — ingestão (publica no JetStream) e consulta (lê do ClickHouse).
//
// main.go é o composition root: monta config → conexões → usecases →
// handlers → servidor HTTP, com graceful shutdown. A API nunca escreve no
// banco: valida, publica no stream e responde — o writer persiste.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/marcoscorrea/ndovu/backend/api"
	"github.com/marcoscorrea/ndovu/backend/internal/adapter/blobstore"
	"github.com/marcoscorrea/ndovu/backend/internal/adapter/clickhouse"
	"github.com/marcoscorrea/ndovu/backend/internal/adapter/ctlpostgres"
	"github.com/marcoscorrea/ndovu/backend/internal/adapter/httpapi"
	"github.com/marcoscorrea/ndovu/backend/internal/adapter/stream"
	"github.com/marcoscorrea/ndovu/backend/internal/config"
	"github.com/marcoscorrea/ndovu/backend/internal/platform"
	"github.com/marcoscorrea/ndovu/backend/internal/usecase"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "erro fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := platform.NewLogger(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Stream (lado de publicação da ingestão) — backend selecionado por
	// NDOVU_STREAM_BACKEND (nats | kafka)
	streamRT, err := stream.New(ctx, stream.Options{
		Backend:              stream.Backend(cfg.StreamBackend),
		NatsURL:              cfg.NatsURL,
		NatsMaxAge:           cfg.StreamMaxAge,
		WriterBatch:          cfg.WriterBatch,
		WriterMaxWait:        cfg.WriterMaxWait,
		KafkaBrokers:         splitCSV(cfg.KafkaBrokers),
		KafkaEventsTopic:     cfg.KafkaEventsTopic,
		KafkaDLQTopic:        cfg.KafkaDLQTopic,
		KafkaConsumerGroup:   cfg.KafkaConsumerGroup,
		KafkaPartitions:      cfg.KafkaPartitions,
		KafkaReplication:     cfg.KafkaReplication,
		KafkaRetention:       cfg.KafkaRetention,
		KafkaMaxPoll:         cfg.KafkaMaxPoll,
		KafkaFetchMaxWait:    cfg.KafkaFetchMaxWait,
		RabbitMQURL:          cfg.RabbitMQURL,
		RabbitMQEventsQueue:  cfg.RabbitMQEventsQueue,
		RabbitMQDLQQueue:     cfg.RabbitMQDLQQueue,
		RabbitMQConsumerName: cfg.RabbitMQConsumerName,
	}, logger)
	if err != nil {
		return fmt.Errorf("inicializando buffer de ingestão: %w", err)
	}
	defer streamRT.Close()

	// ClickHouse (lado de consulta) — o schema é garantido por quem chegar primeiro
	repo, err := clickhouse.Connect(ctx, clickhouse.Options{
		Addr:     cfg.ClickHouseAddr,
		Database: cfg.ClickHouseDB,
		Username: cfg.ClickHouseUser,
		Password: cfg.ClickHousePassword,
	})
	if err != nil {
		return fmt.Errorf("conectando ao ClickHouse: %w", err)
	}
	defer repo.Close()
	if err := repo.EnsureSchema(ctx); err != nil {
		return err
	}

	// Control plane (Postgres): usuários do backoffice e chaves de API
	ctl, err := ctlpostgres.Connect(ctx, cfg.PostgresURL, logger)
	if err != nil {
		return fmt.Errorf("conectando ao Postgres (control plane): %w", err)
	}
	defer ctl.Close()

	// Wiring explícito (composition root)
	metrics := platform.NewMetrics()
	authSvc := usecase.NewAuthService(ctl, ctl, cfg.AuthSecret, cfg.AuthTokenTTL, logger)
	keySvc := usecase.NewAPIKeyService(ctl, cfg.KeyCacheTTL, cfg.IngestRateRPS, logger)
	appSvc := usecase.NewAppService(ctl, keySvc, logger)
	companySvc := usecase.NewCompanyService(ctl, logger)
	issueSvc := usecase.NewIssueService(repo, ctl)
	alertSvc := usecase.NewAlertService(ctl, repo, logger)
	releaseSvc := usecase.NewReleaseService(repo)
	sourceMapSvc := usecase.NewSourceMapService(ctl, logger)
	savedViewSvc := usecase.NewSavedViewService(ctl)
	funnelSvc := usecase.NewFunnelService(ctl, repo)
	retentionSvc := usecase.NewRetentionService(repo)
	auditSvc := usecase.NewAuditService(ctl, logger)
	gdprSvc := usecase.NewGDPRService(repo, logger)
	permissionSvc := usecase.NewPermissionService(ctl, ctl, ctl)
	samplingSvc := usecase.NewSamplingService(ctl, metrics, logger)
	// Load inicial das regras — refresh contínuo roda no writer.
	_ = samplingSvc.Refresh(ctx)
	// Anomaly service — o loop de avaliação roda no writer; aqui só serve
	// os endpoints CRUD/consulta.
	anomalySvc := usecase.NewAnomalyService(ctl, repo, usecase.NewAlertDispatcher(), logger)
	feedbackSvc := usecase.NewFeedbackService(ctl, logger)

	// Session snapshots (Sprint H). Se S3 não configurado, o serviço nasce
	// "desligado" — o handler responde 503 pra POST/GET; o resto da API
	// continua funcionando normalmente.
	var snapshotSvc *usecase.SnapshotService
	if cfg.S3Endpoint != "" {
		blob, err := blobstore.New(ctx, blobstore.Config{
			Endpoint:  cfg.S3Endpoint,
			AccessKey: cfg.S3AccessKey,
			SecretKey: cfg.S3SecretKey,
			Bucket:    cfg.SnapshotBucket,
			UseSSL:    cfg.S3UseSSL,
		})
		if err != nil {
			logger.Warn("snapshots desligados — blob store indisponível", "err", err)
			snapshotSvc = usecase.NewSnapshotService(nil, nil, logger)
		} else {
			snapshotSvc = usecase.NewSnapshotService(ctl, blob, logger)
			logger.Info("snapshots ativos", "bucket", cfg.SnapshotBucket)
		}
	} else {
		snapshotSvc = usecase.NewSnapshotService(nil, nil, logger)
	}
	digestMailer := platform.NewMailer(platform.MailerConfigFromEnv(
		cfg.MailerProvider, cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser,
		cfg.SMTPPassword, cfg.SMTPFrom, cfg.SendGridAPIKey,
	), logger)
	digestSvc := usecase.NewDigestService(repo, issueSvc, digestMailer, usecase.DigestConfig{
		Recipients:   cfg.DigestRecipients,
		From:         cfg.SMTPFrom,
		DashboardURL: cfg.DigestDashboard,
		Weekday:      cfg.DigestWeekday,
		HourUTC:      cfg.DigestHourUTC,
		MinuteUTC:    cfg.DigestMinuteUTC,
	}, logger)
	if err := authSvc.EnsureBootstrapAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword); err != nil {
		return err
	}
	if cfg.Environment != "production" {
		if err := keySvc.EnsureBootstrapKey(ctx, cfg.BootstrapIngestKey, "dev"); err != nil {
			return err
		}
	} else {
		logger.Info("bootstrap de chave de ingestão desativado em produção")
	}

	ingestSvc := usecase.NewIngestService(streamRT.Publisher, logger)
	querySvc := usecase.NewQueryService(repo).WithFeedbackFallback(ctl)
	handlers := httpapi.NewHandlers(ingestSvc, querySvc, authSvc, keySvc, appSvc, companySvc, issueSvc, alertSvc, releaseSvc, sourceMapSvc, savedViewSvc, funnelSvc, retentionSvc, digestSvc, auditSvc, gdprSvc, permissionSvc, samplingSvc, snapshotSvc, anomalySvc, feedbackSvc, logger, cfg)
	router := httpapi.NewRouter(handlers, cfg, authSvc, keySvc, ctl, ctl, metrics, logger, api.SpecFS)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("ndovu-api ouvindo", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("encerrando...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// splitCSV divide "a,b,c" em []string sem vazios/espaços (brokers, origins).
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
