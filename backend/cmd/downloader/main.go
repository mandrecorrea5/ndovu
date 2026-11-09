// Ndovu Downloader — serviço interno que converte vídeos do YouTube em MP3
// (yt-dlp + ffmpeg) e entrega um .zip por job.
//
// Roda separado da API porque depende de python/yt-dlp/ffmpeg, que não estão
// na imagem distroless da API. Só é acessível na rede interna do compose e
// exige o token compartilhado NDOVU_DOWNLOADER_TOKEN.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marcoscorrea/ndovu/backend/internal/downloader"
	"github.com/marcoscorrea/ndovu/backend/internal/platform"
)

type config struct {
	Addr           string
	DataDir        string
	Token          string
	YTDLPBin       string
	FFmpegLocation string
	CookiesPath    string
	MaxVideos      int
	Concurrency    int
	VideoTimeout   time.Duration
	JobTTL         time.Duration
	CleanupEvery   time.Duration
	LogLevel       string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "erro fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	logger := platform.NewLogger(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	svc := downloader.New(downloader.Config{
		DataDir:        cfg.DataDir,
		YTDLPBin:       cfg.YTDLPBin,
		FFmpegLocation: cfg.FFmpegLocation,
		CookiesPath:    cfg.CookiesPath,
		MaxVideos:      cfg.MaxVideos,
		Concurrency:    cfg.Concurrency,
		VideoTimeout:   cfg.VideoTimeout,
		JobTTL:         cfg.JobTTL,
		CleanupEvery:   cfg.CleanupEvery,
	}, logger)
	svc.Start(ctx)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           newRouter(svc, cfg.Token, logger),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("downloader ouvindo", "addr", cfg.Addr, "dataDir", cfg.DataDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func newRouter(svc *downloader.Service, token string, logger *slog.Logger) http.Handler {
	r := chi.NewRouter()
	r.Use(requestLogger(logger))

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Group(func(r chi.Router) {
		r.Use(tokenAuth(token))
		r.Post("/jobs", func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				URLs []string `json:"urls"`
			}
			r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json inválido"})
				return
			}
			job, err := svc.Create(req.URLs)
			if err != nil {
				var verr *downloader.ValidationError
				if errors.As(err, &verr) {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": verr.Msg})
					return
				}
				logger.Error("downloader: criando job", "err", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "falha ao criar job"})
				return
			}
			writeJSON(w, http.StatusAccepted, job)
		})

		r.Get("/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
			job, err := svc.Get(chi.URLParam(r, "id"))
			if errors.Is(err, downloader.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "job não encontrado"})
				return
			}
			writeJSON(w, http.StatusOK, job)
		})

		r.Get("/jobs/{id}/archive", func(w http.ResponseWriter, r *http.Request) {
			path, err := svc.ArchivePath(chi.URLParam(r, "id"))
			if errors.Is(err, downloader.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "job não encontrado"})
				return
			}
			if errors.Is(err, downloader.ErrNotReady) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			f, err := os.Open(path)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "arquivo indisponível"})
				return
			}
			defer f.Close()
			info, err := f.Stat()
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "arquivo indisponível"})
				return
			}
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Content-Disposition", `attachment; filename="ndovu-mp3.zip"`)
			w.Header().Set("Cache-Control", "no-store")
			http.ServeContent(w, r, "ndovu-mp3.zip", info.ModTime(), f)
		})
	})

	return r
}

func tokenAuth(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !constantTimeEqual(r.Header.Get("X-Downloader-Token"), token) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "token inválido"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			logger.Debug("downloader: request", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start))
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func loadConfig() (config, error) {
	token := strings.TrimSpace(os.Getenv("NDOVU_DOWNLOADER_TOKEN"))
	if token == "" {
		return config{}, errors.New("NDOVU_DOWNLOADER_TOKEN é obrigatório (token compartilhado com a API)")
	}
	return config{
		Addr:           envStr("NDOVU_DOWNLOADER_ADDR", ":8090"),
		DataDir:        envStr("NDOVU_DOWNLOADER_DATA_DIR", "/data/downloads"),
		Token:          token,
		YTDLPBin:       envStr("NDOVU_YTDLP_BIN", "yt-dlp"),
		FFmpegLocation: envStr("NDOVU_FFMPEG_LOCATION", ""),
		CookiesPath:    envStr("NDOVU_YTDLP_COOKIES", ""),
		MaxVideos:      envInt("NDOVU_DOWNLOADER_MAX_VIDEOS", 10),
		Concurrency:    envInt("NDOVU_DOWNLOADER_CONCURRENCY", 1),
		VideoTimeout:   time.Duration(envInt("NDOVU_DOWNLOADER_VIDEO_TIMEOUT_SECONDS", 600)) * time.Second,
		JobTTL:         time.Duration(envInt("NDOVU_DOWNLOADER_JOB_TTL_HOURS", 24)) * time.Hour,
		CleanupEvery:   time.Duration(envInt("NDOVU_DOWNLOADER_CLEANUP_SECONDS", 600)) * time.Second,
		LogLevel:       envStr("NDOVU_LOG_LEVEL", "info"),
	}, nil
}

func envStr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil {
		return def
	}
	return v
}
