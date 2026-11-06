package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/marcoscorrea/ndovu/backend/internal/adapter/downloaderclient"
	"github.com/marcoscorrea/ndovu/backend/internal/config"
)

type fakeDownloader struct {
	created atomic.Int32
	lastURL []string
	archive []byte
}

func (f *fakeDownloader) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("X-Downloader-Token") != "segredo" {
			http.Error(w, `{"error":"token inválido"}`, http.StatusUnauthorized)
			return
		}
		f.created.Add(1)
		var body struct {
			URLs []string `json:"urls"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.lastURL = body.URLs
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "job-1", "status": "queued", "total": len(body.URLs),
		})
	})
	mux.HandleFunc("/jobs/job-1", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "job-1", "status": "done", "total": 1, "done": 1, "archiveBytes": len(f.archive),
		})
	})
	mux.HandleFunc("/jobs/job-1/archive", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(f.archive)
	})
	mux.HandleFunc("/jobs/missing", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"job não encontrado"}`))
	})
	return mux
}

func newDownloadTestRouter(t *testing.T) (http.Handler, *fakeDownloader) {
	t.Helper()
	fake := &fakeDownloader{archive: []byte("PK\x03\x04fakezip")}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	h := &Handlers{
		downloader: downloaderclient.New(srv.URL, "segredo"),
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		cfg:        config.Config{DownloaderMaxVideos: 10},
	}
	r := chi.NewRouter()
	r.Post("/downloads", h.PostDownloadJob)
	r.Get("/downloads/{id}", h.GetDownloadJob)
	r.Get("/downloads/{id}/archive", h.GetDownloadArchive)
	return r, fake
}

func TestPostDownloadJob(t *testing.T) {
	router, fake := newDownloadTestRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/downloads",
		strings.NewReader(`{"urls":["https://youtu.be/dQw4w9WgXcQ"]}`))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if fake.created.Load() != 1 {
		t.Fatalf("downloader deveria ter recebido 1 job, recebeu %d", fake.created.Load())
	}
	if len(fake.lastURL) != 1 || !strings.Contains(fake.lastURL[0], "dQw4w9WgXcQ") {
		t.Fatalf("urls repassadas = %v", fake.lastURL)
	}

	var job downloaderclient.Job
	if err := json.Unmarshal(rr.Body.Bytes(), &job); err != nil {
		t.Fatalf("json: %v", err)
	}
	if job.ID != "job-1" || job.Status != "queued" {
		t.Fatalf("job = %+v", job)
	}
}

func TestPostDownloadJobRejectsInvalidURL(t *testing.T) {
	router, fake := newDownloadTestRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/downloads",
		strings.NewReader(`{"urls":["https://vimeo.com/123"]}`))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if fake.created.Load() != 0 {
		t.Fatal("downloader não deveria ser chamado para URL inválida")
	}
}

func TestGetDownloadJob(t *testing.T) {
	router, _ := newDownloadTestRouter(t)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/downloads/job-1", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"done"`) {
		t.Fatalf("body = %s", rr.Body.String())
	}
}

func TestGetDownloadJobNotFound(t *testing.T) {
	router, _ := newDownloadTestRouter(t)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/downloads/missing", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
}

func TestGetDownloadArchive(t *testing.T) {
	router, fake := newDownloadTestRouter(t)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/downloads/job-1/archive", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != string(fake.archive) {
		t.Fatalf("body = %q", rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("content-type = %q", ct)
	}
	if cd := rr.Header().Get("Content-Disposition"); !strings.Contains(cd, "ndovu-mp3-job-1.zip") {
		t.Fatalf("content-disposition = %q", cd)
	}
}

func TestDownloadsDisabled(t *testing.T) {
	h := &Handlers{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		cfg:    config.Config{DownloaderMaxVideos: 10},
	}
	r := chi.NewRouter()
	r.Post("/downloads", h.PostDownloadJob)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/downloads",
		strings.NewReader(`{"urls":["https://youtu.be/dQw4w9WgXcQ"]}`)))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
}
