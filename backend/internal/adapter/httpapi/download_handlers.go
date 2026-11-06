package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/marcoscorrea/ndovu/backend/internal/adapter/downloaderclient"
	"github.com/marcoscorrea/ndovu/backend/internal/downloader"
)

// PostDownloadJob cria um job de extração de MP3. Restrito a admin (o grupo
// /v1/admin já aplica bearerAuth + requireRole).
func (h *Handlers) PostDownloadJob(w http.ResponseWriter, r *http.Request) {
	if !h.downloader.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "downloader desabilitado — defina NDOVU_DOWNLOADER_URL"})
		return
	}

	var req struct {
		URLs []string `json:"urls"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "json inválido"})
		return
	}

	urls, err := downloader.ValidateURLs(req.URLs, h.cfg.DownloaderMaxVideos)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}

	job, err := h.downloader.Create(r.Context(), urls)
	if err != nil {
		h.writeDownloadError(w, err)
		return
	}

	h.recordAudit(r, "download.create", "download_job", job.ID, map[string]any{"videos": len(urls)})
	writeJSON(w, http.StatusAccepted, job)
}

// GetDownloadJob devolve o progresso de um job.
func (h *Handlers) GetDownloadJob(w http.ResponseWriter, r *http.Request) {
	if !h.downloader.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "downloader desabilitado"})
		return
	}
	job, err := h.downloader.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeDownloadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// GetDownloadArchive faz streaming do zip pronto.
func (h *Handlers) GetDownloadArchive(w http.ResponseWriter, r *http.Request) {
	if !h.downloader.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "downloader desabilitado"})
		return
	}
	id := chi.URLParam(r, "id")
	res, err := h.downloader.Archive(r.Context(), id)
	if err != nil {
		h.writeDownloadError(w, err)
		return
	}
	defer res.Body.Close()

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="ndovu-mp3-%s.zip"`, id))
	w.Header().Set("Cache-Control", "no-store")
	if cl := res.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, res.Body)
}

// writeDownloadError traduz o erro do cliente para o status original do
// downloader (400/404/409/502/503).
func (h *Handlers) writeDownloadError(w http.ResponseWriter, err error) {
	var de *downloaderclient.Error
	if errors.As(err, &de) {
		writeJSON(w, de.Status, errorResponse{Error: de.Message})
		return
	}
	h.logger.Error("downloader: erro inesperado", "err", err)
	writeJSON(w, http.StatusBadGateway, errorResponse{Error: "falha ao falar com o downloader"})
}
