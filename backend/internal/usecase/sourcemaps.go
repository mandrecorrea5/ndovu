package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/go-sourcemap/sourcemap"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// SourceMapService faz upload, listagem, remoção e resolução de stack traces
// minificados usando os source maps armazenados. Um cache LRU-ish em memória
// evita reparsear source maps a cada erro.
type SourceMapService struct {
	store  domain.SourceMapStore
	logger *slog.Logger

	mu    sync.RWMutex
	cache map[string]*sourcemap.Consumer // key: app|release|filename
}

// NewSourceMapService cria o serviço.
func NewSourceMapService(store domain.SourceMapStore, logger *slog.Logger) *SourceMapService {
	return &SourceMapService{
		store:  store,
		logger: logger,
		cache:  map[string]*sourcemap.Consumer{},
	}
}

// UploadInput é o payload de upload — recebido como JSON base64/texto, não
// multipart, para simplificar o SDK CLI de upload.
type UploadInput struct {
	App        string
	Release    string
	Filename   string
	Content    string // conteúdo do .map (JSON serializado)
	UploadedBy string
}

// Upload valida e persiste um source map. Reenvio sobrescreve.
func (s *SourceMapService) Upload(ctx context.Context, in UploadInput) (domain.SourceMap, error) {
	var issues []string
	if strings.TrimSpace(in.App) == "" {
		issues = append(issues, "app é obrigatório")
	}
	if strings.TrimSpace(in.Release) == "" {
		issues = append(issues, "release é obrigatório")
	}
	if strings.TrimSpace(in.Filename) == "" {
		issues = append(issues, "filename é obrigatório")
	}
	if strings.TrimSpace(in.Content) == "" {
		issues = append(issues, "content vazio")
	}
	if len(issues) > 0 {
		return domain.SourceMap{}, domain.NewValidationError(issues...)
	}

	// Valida se o content parseia como source map ANTES de gravar — pega
	// erros de upload cedo (arquivo truncado, JSON inválido, etc.).
	if _, err := sourcemap.Parse("", []byte(in.Content)); err != nil {
		return domain.SourceMap{}, domain.NewValidationError("source map inválido: " + err.Error())
	}

	m, err := s.store.UpsertSourceMap(ctx, domain.SourceMap{
		App: in.App, Release: in.Release, Filename: in.Filename, UploadedBy: in.UploadedBy,
	}, in.Content)
	if err != nil {
		return domain.SourceMap{}, err
	}
	// Invalida o cache pra próxima resolução buscar a versão nova.
	s.invalidate(in.App, in.Release, in.Filename)
	s.logger.InfoContext(ctx, "source map recebido",
		"app", in.App, "release", in.Release, "filename", in.Filename, "size", m.SizeBytes)
	return m, nil
}

// List devolve o metadata dos source maps armazenados.
func (s *SourceMapService) List(ctx context.Context, app, release string) ([]domain.SourceMap, error) {
	return s.store.ListSourceMaps(ctx, app, release)
}

// Get resolve um source map por id (ownership check no admin).
func (s *SourceMapService) Get(ctx context.Context, id string) (domain.SourceMap, error) {
	return s.store.GetSourceMap(ctx, id)
}

// Delete remove um source map pelo id.
func (s *SourceMapService) Delete(ctx context.Context, id string) error {
	// Cache: como não temos app/release aqui, dropamos tudo.
	// Delete é operação rara — o custo é aceitável.
	if err := s.store.DeleteSourceMap(ctx, id); err != nil {
		return err
	}
	s.mu.Lock()
	s.cache = map[string]*sourcemap.Consumer{}
	s.mu.Unlock()
	return nil
}

// StackFrame representa uma linha do stack trace (bruta ou resolvida).
type StackFrame struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Function string `json:"function,omitempty"`
	Source   string `json:"source,omitempty"` // caminho original (após map)
	Original bool   `json:"original"`         // true se saiu do source map
}

// stackRe captura formatos comuns do stack trace (Chrome/Firefox/Safari):
//
//	at foo (https://cdn.app.com/static/js/main.abc.js:1:1234)
//	foo@https://cdn.app.com/static/js/main.abc.js:1:1234
//	https://cdn.app.com/static/js/main.abc.js:1:1234
var stackRe = regexp.MustCompile(`(?:at\s+(\S+)\s+\()?(?:(\S+?)@)?(https?://\S+?|/\S+?):(\d+):(\d+)\)?`)

// Resolve percorre linhas de um stack trace bruto e devolve frames — trocando
// as linhas minificadas pela posição original via source map quando disponível.
// Se não houver source map para um arquivo, a linha volta como frame bruto
// (Original=false).
func (s *SourceMapService) Resolve(ctx context.Context, app, release, stack string) []StackFrame {
	frames := []StackFrame{}
	for _, line := range strings.Split(stack, "\n") {
		match := stackRe.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		fn := match[1]
		if fn == "" {
			fn = match[2]
		}
		url := match[3]
		lineNum, _ := strconv.Atoi(match[4])
		col, _ := strconv.Atoi(match[5])

		frame := StackFrame{
			File:     url,
			Line:     lineNum,
			Column:   col,
			Function: fn,
		}

		filename := filenameFromURL(url)
		if consumer := s.consumer(ctx, app, release, filename); consumer != nil {
			if source, name, origLine, origCol, ok := consumer.Source(lineNum, col); ok {
				frame.Source = fmt.Sprintf("%s:%d:%d", source, origLine, origCol)
				if name != "" {
					frame.Function = name
				}
				frame.Original = true
			}
		}
		frames = append(frames, frame)
	}
	return frames
}

// consumer carrega o source map (com cache) e devolve o parser pronto.
// Retorna nil quando não há source map para o arquivo (não é erro — usuário
// simplesmente não subiu ainda; a resolução volta a linha bruta).
func (s *SourceMapService) consumer(ctx context.Context, app, release, filename string) *sourcemap.Consumer {
	key := cacheKey(app, release, filename)
	s.mu.RLock()
	c := s.cache[key]
	s.mu.RUnlock()
	if c != nil {
		return c
	}
	content, err := s.store.GetSourceMapContent(ctx, app, release, filename)
	if err != nil {
		return nil
	}
	consumer, err := sourcemap.Parse("", []byte(content))
	if err != nil {
		s.logger.WarnContext(ctx, "source map falhou ao parsear",
			"app", app, "release", release, "filename", filename, "err", err)
		return nil
	}
	s.mu.Lock()
	s.cache[key] = consumer
	s.mu.Unlock()
	return consumer
}

func (s *SourceMapService) invalidate(app, release, filename string) {
	s.mu.Lock()
	delete(s.cache, cacheKey(app, release, filename))
	s.mu.Unlock()
}

func cacheKey(app, release, filename string) string {
	return app + "|" + release + "|" + filename
}

// filenameFromURL extrai só o basename da URL (o que combina com o
// "filename" usado no upload). Ex.: https://cdn/static/js/main.abc.js → main.abc.js
func filenameFromURL(url string) string {
	if i := strings.LastIndex(url, "/"); i >= 0 && i < len(url)-1 {
		return url[i+1:]
	}
	return url
}
