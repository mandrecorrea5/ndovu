package downloader

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateURLs(t *testing.T) {
	valid := []string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"youtu.be/dQw4w9WgXcQ",
		"https://youtube.com/shorts/dQw4w9WgXcQ",
		"https://m.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://music.youtube.com/watch?v=dQw4w9WgXcQ",
	}
	got, err := ValidateURLs(valid, 10)
	if err != nil {
		t.Fatalf("ValidateURLs: %v", err)
	}
	if len(got) != len(valid) {
		t.Fatalf("esperava %d links, veio %d: %v", len(valid), len(got), got)
	}
	if !strings.HasPrefix(got[1], "https://youtu.be/") {
		t.Fatalf("link sem esquema deveria virar https: %s", got[1])
	}

	if _, err := ValidateURLs([]string{"https://vimeo.com/123"}, 10); err == nil {
		t.Fatal("esperava erro para host não-YouTube")
	}
	if _, err := ValidateURLs([]string{"https://youtube.com/playlist?list=abc"}, 10); err == nil {
		t.Fatal("esperava erro para playlist")
	}
	if _, err := ValidateURLs([]string{"  ", ""}, 10); err == nil {
		t.Fatal("esperava erro para lista vazia")
	}
	if _, err := ValidateURLs([]string{
		"https://youtu.be/aaaaaaaaaaa",
		"https://youtu.be/bbbbbbbbbbb",
	}, 1); err == nil {
		t.Fatal("esperava erro acima do máximo")
	}

	dup, err := ValidateURLs([]string{
		"https://youtu.be/dQw4w9WgXcQ",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
	}, 10)
	if err != nil {
		t.Fatalf("ValidateURLs: %v", err)
	}
	if len(dup) != 2 {
		// URLs diferentes apontando para o mesmo vídeo não são deduplicadas
		// por id (só por string normalizada); documenta o comportamento.
		t.Fatalf("dedupe inesperado: %v", dup)
	}
}

func TestJobProducesZip(t *testing.T) {
	svc, _ := newTestService(t, 1)
	job, err := svc.Create([]string{"https://youtu.be/dQw4w9WgXcQ"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	done := waitJob(t, svc, job.ID)
	if done.Status != StatusDone {
		t.Fatalf("status = %s, erro = %q, falhas = %+v", done.Status, done.Error, done.Failed)
	}
	if done.ArchiveBytes == 0 {
		t.Fatal("ArchiveBytes deveria ser > 0")
	}

	path, err := svc.ArchivePath(job.ID)
	if err != nil {
		t.Fatalf("ArchivePath: %v", err)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("abrindo zip: %v", err)
	}
	defer zr.Close()

	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if len(names) != 1 || !strings.HasSuffix(names[0], ".mp3") {
		t.Fatalf("zip deveria ter 1 mp3, veio %v", names)
	}
}

func TestFFmpegLocationFlag(t *testing.T) {
	svc, _ := newTestService(t, 1)
	svc.cfg.FFmpegLocation = "/opt/ffmpeg/bin"

	job, err := svc.Create([]string{"https://youtu.be/dQw4w9WgXcQ"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitJob(t, svc, job.ID)

	args, err := os.ReadFile(filepath.Join(svc.cfg.DataDir, job.ID, "args.txt"))
	if err != nil {
		t.Fatalf("lendo args: %v", err)
	}
	if !strings.Contains(string(args), "--ffmpeg-location\n/opt/ffmpeg/bin") {
		t.Fatalf("args sem --ffmpeg-location:\n%s", args)
	}
}

func TestCookiesFlag(t *testing.T) {
	svc, _ := newTestService(t, 1)
	svc.cfg.CookiesPath = "/secrets/cookies.txt"

	job, err := svc.Create([]string{"https://youtu.be/dQw4w9WgXcQ"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitJob(t, svc, job.ID)

	args, err := os.ReadFile(filepath.Join(svc.cfg.DataDir, job.ID, "args.txt"))
	if err != nil {
		t.Fatalf("lendo args: %v", err)
	}
	if !strings.Contains(string(args), "--cookies\n/secrets/cookies.txt") {
		t.Fatalf("args sem --cookies:\n%s", args)
	}
}

func TestJobPartialFailureWritesFalhas(t *testing.T) {
	svc, _ := newTestService(t, 1)
	job, err := svc.Create([]string{
		"https://youtu.be/dQw4w9WgXcQ",
		"https://youtu.be/badbadbadba",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	done := waitJob(t, svc, job.ID)
	if done.Status != StatusDone {
		t.Fatalf("status = %s, erro = %q", done.Status, done.Error)
	}
	if len(done.Failed) != 1 || !strings.Contains(done.Failed[0].URL, "badbadbadba") {
		t.Fatalf("falhas = %+v", done.Failed)
	}
	if done.Done != 2 {
		t.Fatalf("Done = %d, esperava 2", done.Done)
	}

	path, err := svc.ArchivePath(job.ID)
	if err != nil {
		t.Fatalf("ArchivePath: %v", err)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("abrindo zip: %v", err)
	}
	defer zr.Close()

	found := false
	for _, f := range zr.File {
		if f.Name == "falhas.txt" {
			found = true
			rc, _ := f.Open()
			data, _ := io.ReadAll(rc)
			rc.Close()
			if !strings.Contains(string(data), "badbadbadba") {
				t.Fatalf("falhas.txt sem a URL: %s", data)
			}
		}
	}
	if !found {
		t.Fatal("zip deveria conter falhas.txt")
	}
}

func TestJobAllFailuresIsError(t *testing.T) {
	svc, _ := newTestService(t, 1)
	job, err := svc.Create([]string{"https://youtu.be/badbadbadba"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	done := waitJob(t, svc, job.ID)
	if done.Status != StatusError {
		t.Fatalf("status = %s, esperava error", done.Status)
	}
	if _, err := svc.ArchivePath(job.ID); !errors.Is(err, ErrNotReady) {
		t.Fatalf("ArchivePath = %v, esperava ErrNotReady", err)
	}
}

func TestJobTimeout(t *testing.T) {
	svc, dir := newTestService(t, 1)
	svc.cfg.VideoTimeout = 150 * time.Millisecond
	writeSlowScript(t, dir)

	job, err := svc.Create([]string{"https://youtu.be/dQw4w9WgXcQ"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	done := waitJob(t, svc, job.ID)
	if done.Status != StatusError {
		t.Fatalf("status = %s", done.Status)
	}
	if !strings.Contains(done.Failed[0].Error, "timeout") {
		t.Fatalf("erro = %q, esperava timeout", done.Failed[0].Error)
	}
}

func TestLoadExistingMarksInterrupted(t *testing.T) {
	dataDir := t.TempDir()
	id := "job-antigo"
	dir := filepath.Join(dataDir, id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "job.json"), []byte(`{"id":"job-antigo","status":"running","total":1}`), 0o640); err != nil {
		t.Fatal(err)
	}

	svc := New(Config{DataDir: dataDir, YTDLPBin: "/bin/true"}, slog.Default())
	svc.Start(context.Background())

	job, err := svc.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if job.Status != StatusError || !strings.Contains(job.Error, "restart") {
		t.Fatalf("job = %+v", job)
	}
}

func TestCleanupRemovesExpired(t *testing.T) {
	svc, _ := newTestService(t, 1)
	job, err := svc.Create([]string{"https://youtu.be/dQw4w9WgXcQ"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitJob(t, svc, job.ID)

	// Envelhece o job em vez de mexer no relógio do serviço (evita corrida
	// com a goroutine do job).
	svc.mu.Lock()
	svc.jobs[job.ID].CreatedAt = time.Now().Add(-48 * time.Hour)
	svc.mu.Unlock()
	svc.cfg.JobTTL = 24 * time.Hour
	svc.cleanup()

	if _, err := svc.Get(job.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get = %v, esperava ErrNotFound", err)
	}
	if _, err := os.Stat(filepath.Join(svc.cfg.DataDir, job.ID)); !os.IsNotExist(err) {
		t.Fatalf("diretório do job deveria ter sido removido: %v", err)
	}
}

// ---------------------------------------------------------------------------

func newTestService(t *testing.T, concurrency int) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "yt-dlp")
	script := `#!/bin/sh
printf '%s\n' "$@" > args.txt
mkdir -p audio
last=""
for a in "$@"; do last="$a"; done
case "$last" in
  *badbadbadba*) echo "erro simulado" >&2; exit 1 ;;
esac
echo "fake-mp3" > "audio/video.mp3"
`
	if err := os.WriteFile(bin, []byte(script), 0o750); err != nil {
		t.Fatal(err)
	}
	svc := New(Config{
		DataDir:      filepath.Join(dir, "data"),
		YTDLPBin:     bin,
		Concurrency:  concurrency,
		VideoTimeout: 5 * time.Second,
		JobTTL:       time.Hour,
	}, slog.Default())
	return svc, dir
}

func writeSlowScript(t *testing.T, dir string) {
	t.Helper()
	bin := filepath.Join(dir, "yt-dlp")
	script := "#!/bin/sh\nsleep 5\n"
	if err := os.WriteFile(bin, []byte(script), 0o750); err != nil {
		t.Fatal(err)
	}
}

func waitJob(t *testing.T, svc *Service, id string) *Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		job, err := svc.Get(id)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if job.Status == StatusDone || job.Status == StatusError {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s não terminou", id)
	return nil
}
