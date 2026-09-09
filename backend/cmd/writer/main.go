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
	"strings"
	"syscall"

	"github.com/marcoscorrea/ndovu/backend/internal/adapter/clickhouse"
	"github.com/marcoscorrea/ndovu/backend/internal/adapter/ctlpostgres"
	"github.com/marcoscorrea/ndovu/backend/internal/adapter/stream"
	"github.com/marcoscorrea/ndovu/backend/internal/config"
	"github.com/marcoscorrea/ndovu/backend/internal/domain"
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

	// Buffer de ingestão — backend selecionado por NDOVU_STREAM_BACKEND
	// (nats | kafka). O consumer drena lotes e o DeadLetter reencaminha os
	// rejeitados pelo armazém.
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

	writeSvc := usecase.NewWriteService(repo, dlqSink{streamRT}, logger)

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
		"backend", cfg.StreamBackend,
		"batch", cfg.WriterBatch,
	)
	return streamRT.Consumer.Run(ctx, writeSvc.HandleBatches)
}

// dlqSink adapta o DeadLetter do Runtime à interface usecase.DeadLetterSink.
type dlqSink struct{ rt *stream.Runtime }

func (d dlqSink) DeadLetter(ctx context.Context, batch domain.IngestBatch, reason string) error {
	return d.rt.DeadLetter(ctx, batch, reason)
}

// splitCSV divide "a,b,c" em []string sem vazios/espaços (brokers).
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
