// Package downloaderclient fala com o serviço interno ndovu-downloader
// (cmd/downloader) por HTTP. A API nunca executa yt-dlp: só valida a entrada,
// repassa o job e faz streaming do zip.
package downloaderclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Job é o estado do job devolvido pelo downloader.
type Job struct {
	ID           string    `json:"id"`
	Status       string    `json:"status"`
	Total        int       `json:"total"`
	Done         int       `json:"done"`
	Failed       []Failure `json:"failed,omitempty"`
	Error        string    `json:"error,omitempty"`
	ArchiveBytes int64     `json:"archiveBytes,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Failure é um vídeo que não pôde ser convertido.
type Failure struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

// Error carrega o status HTTP devolvido pelo downloader para o handler
// repassar ao browser (400 de validação, 404 de job inexistente, 409 de zip
// ainda não pronto).
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

// Client é o cliente HTTP do downloader.
type Client struct {
	baseURL string
	token   string
	hc      *http.Client // JSON curto (criar/consultar)
	stream  *http.Client // zip: sem timeout total, senão corta download grande
}

// New cria o cliente. baseURL vazio deixa o cliente desligado (Enabled=false).
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		hc:      &http.Client{Timeout: 30 * time.Second},
		stream:  &http.Client{},
	}
}

// Enabled indica se o downloader está configurado.
func (c *Client) Enabled() bool { return c != nil && c.baseURL != "" }

// Create agenda um job com os links informados.
func (c *Client) Create(ctx context.Context, urls []string) (*Job, error) {
	body, err := json.Marshal(map[string]any{"urls": urls})
	if err != nil {
		return nil, err
	}
	res, err := c.do(ctx, c.hc, http.MethodPost, "/jobs", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var job Job
	if err := decode(res, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// Get consulta o estado de um job.
func (c *Client) Get(ctx context.Context, id string) (*Job, error) {
	res, err := c.do(ctx, c.hc, http.MethodGet, "/jobs/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var job Job
	if err := decode(res, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// Archive abre o zip do job. O caller fecha o Body.
func (c *Client) Archive(ctx context.Context, id string) (*http.Response, error) {
	return c.do(ctx, c.stream, http.MethodGet, "/jobs/"+url.PathEscape(id)+"/archive", nil)
}

func (c *Client) do(ctx context.Context, hc *http.Client, method, path string, body io.Reader) (*http.Response, error) {
	if !c.Enabled() {
		return nil, &Error{Status: http.StatusServiceUnavailable, Message: "downloader não configurado"}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Downloader-Token", c.token)

	res, err := hc.Do(req)
	if err != nil {
		return nil, &Error{Status: http.StatusBadGateway, Message: "downloader indisponível"}
	}
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return res, nil
	}

	defer res.Body.Close()
	var payload struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 8<<10)).Decode(&payload)
	msg := payload.Error
	if msg == "" {
		msg = fmt.Sprintf("downloader respondeu HTTP %d", res.StatusCode)
	}
	return nil, &Error{Status: res.StatusCode, Message: msg}
}

func decode(res *http.Response, out any) error {
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out); err != nil {
		return &Error{Status: http.StatusBadGateway, Message: "resposta inválida do downloader"}
	}
	return nil
}
