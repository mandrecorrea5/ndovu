// Ndovu Writer — consome lotes do JetStream e persiste em bulk no ClickHouse.
//
// É um processo separado da API de propósito: escala independente, absorve
// picos drenando o stream no ritmo do banco, e uma indisponibilidade do
// ClickHouse nunca derruba a ingestão (as mensagens ficam no stream).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/marcoscorrea/ndovu/backend/internal/adapter/clickhouse"
	"github.com/marcoscorrea/ndovu/backend/internal/adapter/ctlpostgres"
	"github.com/marcoscorrea/ndovu/backend/internal/adapter/natsstream"
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
	cfg, err := config.LoadFor("writer")
	if err != nil {
		return err
	}
	logger := platform.NewLogger(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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

	nc, err := natsstream.Connect(cfg.NatsURL)
	if err != nil {
		return fmt.Errorf("conectando ao NATS: %w", err)
	}
	defer nc.Close()
	js, err := natsstream.EnsureStream(ctx, nc, cfg.StreamMaxAge)
	if err != nil {
		return err
	}
	consumer, err := natsstream.NewConsumer(ctx, js, cfg.WriterBatch, cfg.WriterMaxWait, logger)
	if err != nil {
		return err
	}

	publisher := natsstream.NewPublisher(js) // usado como DLQ de lotes rejeitados
	writeSvc := usecase.NewWriteService(repo, publisher, logger)

	// Sampling adaptativo (Sprint G): filtra lotes antes de persistir.
	// Requer control plane. Se estiver fora, WriteService segue sem sampling
	// (aceita tudo — falha aberta é preferível a bloquear ingestão).
	if samplingStore, err := ctlpostgres.Connect(ctx, cfg.PostgresURL, logger); err == nil {
		defer samplingStore.Close()
		samplingSvc := usecase.NewSamplingService(samplingStore, nil, logger)
		go samplingSvc.Run(ctx, cfg.SamplingRefresh)
		writeSvc = writeSvc.WithSampling(samplingSvc)
	} else {
		logger.Warn("sampling desligado — control plane indisponível", "err", err)
	}

	// Avaliador de alertas + digest semanal — rodam no writer para não duplicar
	// entre réplicas da API. Se não conseguir conectar no control plane,
	// seguimos sem esses dois (o writer continua persistindo eventos).
	ctl, err := ctlpostgres.Connect(ctx, cfg.PostgresURL, logger)
	if err != nil {
		logger.Warn("control plane indisponível — alertas/digest desligados", "err", err)
	} else {
		defer ctl.Close()
		alertSvc := usecase.NewAlertService(ctl, repo, logger)
		go alertSvc.Run(ctx, cfg.AlertsInterval)

		// Detecção de anomalia (Fase 4 sprint I) — roda no writer para não
		// duplicar entre réplicas da API.
		anomalySvc := usecase.NewAnomalyService(ctl, repo, usecase.NewAlertDispatcher(), logger)
		go anomalySvc.Run(ctx, cfg.AnomalyInterval)

		issueSvc := usecase.NewIssueService(repo, ctl)
		mailer := platform.NewMailer(platform.MailerConfigFromEnv(
			cfg.MailerProvider, cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser,
			cfg.SMTPPassword, cfg.SMTPFrom, cfg.SendGridAPIKey,
		), logger)
		digestSvc := usecase.NewDigestService(repo, issueSvc, mailer, usecase.DigestConfig{
			Recipients:   cfg.DigestRecipients,
			From:         cfg.SMTPFrom,
			DashboardURL: cfg.DigestDashboard,
			Weekday:      cfg.DigestWeekday,
			HourUTC:      cfg.DigestHourUTC,
			MinuteUTC:    cfg.DigestMinuteUTC,
		}, logger)
		go digestSvc.Run(ctx, cfg.DigestTickEvery)
	}

	logger.Info("ndovu-writer consumindo",
		"stream", natsstream.StreamName,
		"batch", cfg.WriterBatch,
	)
	return consumer.Run(ctx, writeSvc.HandleBatches)
}
