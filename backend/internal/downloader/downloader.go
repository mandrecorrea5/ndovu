// Package downloader executa jobs de extração de áudio (MP3) de vídeos do
// YouTube usando yt-dlp + ffmpeg.
//
// O serviço é interno e roda como processo separado (cmd/downloader): a API
// nunca executa yt-dlp. Cada job vive num diretório próprio em DataDir com
// job.json, audio/*.mp3 e bundle.zip; o estado é persistido a cada transição
// para sobreviver a restart (jobs interrompidos viram erro na subida).
package downloader

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Status é o ciclo de vida de um job.
type Status string

const (
	StatusQueued  Status = "queued"
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusError   Status = "error"
)

// Failure registra um vídeo que não pôde ser convertido.
type Failure struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

// Job é o estado público de uma extração. O JSON é o contrato com a API.
type Job struct {
	ID           string    `json:"id"`
	Status       Status    `json:"status"`
	Total        int       `json:"total"`
	Done         int       `json:"done"`
	Failed       []Failure `json:"failed,omitempty"`
	Error        string    `json:"error,omitempty"`
	ArchiveBytes int64     `json:"archiveBytes,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Config parametriza o serviço.
type Config struct {
	DataDir        string        // diretório raiz dos jobs
	YTDLPBin       string        // binário do yt-dlp (default "yt-dlp")
	FFmpegLocation string        // diretório/binário do ffmpeg (vazio = PATH)
	MaxVideos      int           // teto de vídeos por job
	Concurrency    int           // jobs simultâneos
	VideoTimeout   time.Duration // timeout por vídeo
	JobTTL         time.Duration // retenção do diretório do job
	CleanupEvery   time.Duration // intervalo do GC
}

// ValidationError indica entrada inválida (HTTP 400).
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

var (
	// ErrNotFound indica job inexistente.
	ErrNotFound = errors.New("job não encontrado")
	// ErrNotReady indica que o zip ainda não está disponível.
	ErrNotReady = errors.New("arquivo ainda não está pronto")
)

// Service guarda os jobs em memória e no disco.
type Service struct {
	cfg    Config
	logger *slog.Logger

	mu   sync.RWMutex
	jobs map[string]*Job
	sem  chan struct{}

	ctx    context.Context
	runCmd func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// New cria o serviço. Chame Start para carregar jobs antigos e ligar o GC.
func New(cfg Config, logger *slog.Logger) *Service {
	if cfg.YTDLPBin == "" {
		cfg.YTDLPBin = "yt-dlp"
	}
	if cfg.MaxVideos <= 0 {
		cfg.MaxVideos = 10
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	if cfg.VideoTimeout <= 0 {
		cfg.VideoTimeout = 10 * time.Minute
	}
	if cfg.JobTTL <= 0 {
		cfg.JobTTL = 24 * time.Hour
	}
	if cfg.CleanupEvery <= 0 {
		cfg.CleanupEvery = 10 * time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		cfg:    cfg,
		logger: logger,
		jobs:   make(map[string]*Job),
		sem:    make(chan struct{}, cfg.Concurrency),
		ctx:    context.Background(),
		runCmd: exec.CommandContext,
	}
}

// Start carrega os jobs persistidos, marca os interrompidos como erro e
// inicia o coletor de diretórios expirados.
func (s *Service) Start(ctx context.Context) {
	s.ctx = ctx
	if err := os.MkdirAll(s.cfg.DataDir, 0o750); err != nil {
		s.logger.Error("downloader: criando diretório de dados", "err", err)
	}
	s.loadExisting()
	go s.cleanupLoop(ctx)
}

// Create valida os links, cria o job e agenda a execução.
func (s *Service) Create(urls []string) (*Job, error) {
	clean, err := ValidateURLs(urls, s.cfg.MaxVideos)
	if err != nil {
		return nil, err
	}

	id := uuid.NewString()
	dir := filepath.Join(s.cfg.DataDir, id)
	if err := os.MkdirAll(filepath.Join(dir, "audio"), 0o750); err != nil {
		return nil, fmt.Errorf("preparando diretório do job: %w", err)
	}

	now := time.Now()
	job := &Job{
		ID:        id,
		Status:    StatusQueued,
		Total:     len(clean),
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.mu.Lock()
	s.jobs[id] = job
	s.mu.Unlock()
	s.persist(job)

	go s.run(s.ctx, id, dir, clean)
	return s.Get(id)
}

// Get devolve uma cópia do job.
func (s *Service) Get(id string) (*Job, error) {
	s.mu.RLock()
	job, ok := s.jobs[id]
	var snapshot *Job
	if ok {
		// Clone DENTRO do lock: a goroutine do job escreve no mesmo struct.
		snapshot = cloneJob(job)
	}
	s.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	return snapshot, nil
}

// ArchivePath devolve o caminho do zip de um job concluído.
func (s *Service) ArchivePath(id string) (string, error) {
	job, err := s.Get(id)
	if err != nil {
		return "", err
	}
	switch job.Status {
	case StatusDone:
	case StatusError:
		return "", fmt.Errorf("%w: %s", ErrNotReady, job.Error)
	default:
		return "", ErrNotReady
	}
	path := filepath.Join(s.cfg.DataDir, id, "bundle.zip")
	if _, err := os.Stat(path); err != nil {
		return "", ErrNotReady
	}
	return path, nil
}

func (s *Service) run(ctx context.Context, id, dir string, urls []string) {
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-ctx.Done():
		s.fail(id, "serviço encerrando")
		return
	}

	s.update(id, func(j *Job) { j.Status = StatusRunning })

	for _, u := range urls {
		if ctx.Err() != nil {
			s.update(id, func(j *Job) {
				j.Done++
				j.Failed = append(j.Failed, Failure{URL: u, Error: "job cancelado"})
			})
			continue
		}

		vctx, cancel := context.WithTimeout(ctx, s.cfg.VideoTimeout)
		err := s.downloadOne(vctx, dir, u)
		cancel()

		s.update(id, func(j *Job) {
			j.Done++
			if err != nil {
				j.Failed = append(j.Failed, Failure{URL: u, Error: err.Error()})
			}
		})
		if err != nil {
			s.logger.Warn("downloader: vídeo falhou", "job", id, "url", u, "err", err)
		}
	}

	job, err := s.Get(id)
	if err != nil {
		return
	}
	if len(job.Failed) == job.Total {
		s.fail(id, "nenhum áudio foi convertido")
		return
	}

	archive := filepath.Join(dir, "bundle.zip")
	if err := zipDir(filepath.Join(dir, "audio"), archive, job.Failed); err != nil {
		s.fail(id, "montando zip: "+err.Error())
		return
	}
	info, err := os.Stat(archive)
	if err != nil {
		s.fail(id, "montando zip: "+err.Error())
		return
	}

	s.update(id, func(j *Job) {
		j.Status = StatusDone
		j.ArchiveBytes = info.Size()
	})
	s.logger.Info("downloader: job concluído", "job", id, "arquivos", job.Total-len(job.Failed), "bytes", info.Size())
}

func (s *Service) downloadOne(ctx context.Context, dir, videoURL string) error {
	args := []string{
		"--no-playlist",
		"--no-progress",
		"--no-warnings",
		"--newline",
		"--extract-audio",
		"--audio-format", "mp3",
		"--audio-quality", "0",
		"--restrict-filenames",
		"--no-mtime",
		"--no-part",
		"--output", filepath.Join("audio", "%(title).80s-%(id)s.%(ext)s"),
	}
	if s.cfg.FFmpegLocation != "" {
		args = append(args, "--ffmpeg-location", s.cfg.FFmpegLocation)
	}
	args = append(args, videoURL)

	cmd := s.runCmd(ctx, s.cfg.YTDLPBin, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("timeout após %s", s.cfg.VideoTimeout)
	}
	if err != nil {
		return fmt.Errorf("yt-dlp: %s", tail(out.String(), 800))
	}
	return nil
}

func (s *Service) fail(id, msg string) {
	s.update(id, func(j *Job) {
		j.Status = StatusError
		j.Error = msg
	})
}

func (s *Service) update(id string, fn func(*Job)) {
	s.mu.Lock()
	job, ok := s.jobs[id]
	if ok {
		fn(job)
		job.UpdatedAt = time.Now()
	}
	var snapshot *Job
	if ok {
		snapshot = cloneJob(job)
	}
	s.mu.Unlock()
	if snapshot != nil {
		s.persist(snapshot)
	}
}

func (s *Service) persist(job *Job) {
	dir := filepath.Join(s.cfg.DataDir, job.ID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		s.logger.Warn("downloader: persistindo job", "job", job.ID, "err", err)
		return
	}
	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return
	}
	tmp := filepath.Join(dir, "job.json.tmp")
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		s.logger.Warn("downloader: persistindo job", "job", job.ID, "err", err)
		return
	}
	if err := os.Rename(tmp, filepath.Join(dir, "job.json")); err != nil {
		s.logger.Warn("downloader: persistindo job", "job", job.ID, "err", err)
	}
}

func (s *Service) loadExisting() {
	entries, err := os.ReadDir(s.cfg.DataDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.cfg.DataDir, entry.Name(), "job.json"))
		if err != nil {
			continue
		}
		var job Job
		if err := json.Unmarshal(data, &job); err != nil || job.ID == "" {
			continue
		}
		if job.Status == StatusQueued || job.Status == StatusRunning {
			job.Status = StatusError
			job.Error = "interrompido por restart do serviço"
			job.UpdatedAt = time.Now()
		}
		s.jobs[job.ID] = &job
		s.persist(&job)
	}
}

func (s *Service) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.CleanupEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

func (s *Service) cleanup() {
	cutoff := time.Now().Add(-s.cfg.JobTTL)

	s.mu.Lock()
	expired := make([]string, 0)
	for id, job := range s.jobs {
		if job.CreatedAt.Before(cutoff) {
			expired = append(expired, id)
			delete(s.jobs, id)
		}
	}
	s.mu.Unlock()

	for _, id := range expired {
		if err := os.RemoveAll(filepath.Join(s.cfg.DataDir, id)); err != nil {
			s.logger.Warn("downloader: limpando job", "job", id, "err", err)
		}
	}
}

// zipDir compacta os .mp3 de srcDir em dst. Se houver falhas, grava também
// um falhas.txt para o admin ver o que não converteu.
func zipDir(srcDir, dst string, failures []Failure) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	zw := zip.NewWriter(out)
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".mp3") {
			continue
		}
		path := filepath.Join(srcDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = entry.Name()
		header.Method = zip.Store // mp3 já é comprimido
		w, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, f); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}

	if len(failures) > 0 {
		var b strings.Builder
		b.WriteString("Vídeos que não puderam ser convertidos:\n\n")
		for _, f := range failures {
			fmt.Fprintf(&b, "- %s\n  %s\n", f.URL, f.Error)
		}
		w, err := zw.Create("falhas.txt")
		if err != nil {
			return err
		}
		if _, err := io.WriteString(w, b.String()); err != nil {
			return err
		}
	}

	return zw.Close()
}

// ValidateURLs normaliza e valida a lista de links (somente YouTube).
func ValidateURLs(raw []string, max int) ([]string, error) {
	if max <= 0 {
		max = 10
	}
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))

	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		normalized, err := normalizeYouTubeURL(item)
		if err != nil {
			return nil, err
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}

	if len(out) == 0 {
		return nil, &ValidationError{Msg: "informe ao menos um link do YouTube"}
	}
	if len(out) > max {
		return nil, &ValidationError{Msg: fmt.Sprintf("máximo de %d vídeos por job", max)}
	}
	return out, nil
}

func normalizeYouTubeURL(raw string) (string, error) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", &ValidationError{Msg: fmt.Sprintf("link inválido: %s", raw)}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", &ValidationError{Msg: fmt.Sprintf("link inválido: %s", raw)}
	}

	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	switch host {
	case "youtu.be":
		if len(strings.Trim(u.Path, "/")) < 3 {
			return "", &ValidationError{Msg: fmt.Sprintf("link sem id de vídeo: %s", raw)}
		}
	case "youtube.com", "m.youtube.com", "music.youtube.com":
		path := u.Path
		ok := strings.HasPrefix(path, "/watch") && u.Query().Get("v") != ""
		ok = ok || strings.HasPrefix(path, "/shorts/")
		ok = ok || strings.HasPrefix(path, "/live/")
		ok = ok || strings.HasPrefix(path, "/embed/")
		if !ok {
			return "", &ValidationError{Msg: fmt.Sprintf("link não é de um vídeo: %s", raw)}
		}
	default:
		return "", &ValidationError{Msg: fmt.Sprintf("apenas links do YouTube são aceitos: %s", raw)}
	}

	u.Scheme = "https"
	u.Fragment = ""
	return u.String(), nil
}

func cloneJob(job *Job) *Job {
	cp := *job
	if job.Failed != nil {
		cp.Failed = append([]Failure(nil), job.Failed...)
	}
	return &cp
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
