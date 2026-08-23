package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
	"github.com/marcoscorrea/ndovu/backend/internal/platform"
	"github.com/marcoscorrea/ndovu/backend/internal/usecase"
)

type contextKey string

// identityKey guarda a identidade autenticada no contexto da request.
const identityKey contextKey = "ndovu.identity"

// apiKeyContextKey guarda a chave de API validada no contexto da request.
const apiKeyContextKey contextKey = "ndovu.apikey"

// appScopeKey guarda a whitelist de apps que o user autenticado pode ver
// (multi-tenancy Sprint E.3). nil = sem restrição (super-admin).
const appScopeKey contextKey = "ndovu.appscope"

// IdentityFrom recupera a identidade autenticada do contexto.
func IdentityFrom(ctx context.Context) (domain.Identity, bool) {
	id, ok := ctx.Value(identityKey).(domain.Identity)
	return id, ok
}

// APIKeyFrom recupera a chave de API validada (com o app dono) do contexto.
func APIKeyFrom(ctx context.Context) (domain.APIKey, bool) {
	k, ok := ctx.Value(apiKeyContextKey).(domain.APIKey)
	return k, ok
}

// AppScopeFrom recupera a whitelist de apps que o user pode consultar.
// Retorna (nil, true) para super-admin (sem restrição).
func AppScopeFrom(ctx context.Context) ([]string, bool) {
	v := ctx.Value(appScopeKey)
	if v == nil {
		return nil, false
	}
	s, _ := v.([]string)
	return s, true
}

// apiKeyAuth valida X-Api-Key contra o control plane (com cache no serviço) e
// injeta a chave validada no contexto — o handler usa o app dono para vincular
// o envelope ao frontend emissor. Aplica rate limit por chave: 429 com
// Retry-After quando estourar (protege a ingestão de uma chave abusiva).
func apiKeyAuth(keys *usecase.APIKeyService, metrics *platform.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key, err := keys.Validate(r.Context(), r.Header.Get("X-Api-Key"))
			if err != nil {
				if metrics != nil {
					metrics.Counter("ndovu_ingest_auth_rejected_total", "Ingest requests rejected by auth", nil, 1)
				}
				writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "X-Api-Key ausente, inválida ou revogada"})
				return
			}
			if !keys.Allow(key) {
				if metrics != nil {
					metrics.Counter("ndovu_ingest_rate_limited_total", "Ingest requests rejected by rate limit",
						map[string]string{"app": key.App}, 1)
				}
				w.Header().Set("Retry-After", "1")
				writeJSON(w, http.StatusTooManyRequests, errorResponse{Error: "rate limit da chave excedido — aguarde e reenvie"})
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), apiKeyContextKey, key)))
		})
	}
}

// bearerAuth exige um token válido (Authorization: Bearer <jwt>) e injeta a
// identidade no contexto. A verificação fica atrás do port TokenVerifier —
// trocar por Keycloak/OIDC não muda este middleware.
func bearerAuth(verifier domain.TokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			token, ok := strings.CutPrefix(header, "Bearer ")
			if !ok || token == "" {
				writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "token ausente"})
				return
			}
			identity, err := verifier.Verify(r.Context(), token)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "token inválido ou expirado"})
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey, identity)))
		})
	}
}

// tenantScope injeta a whitelist de apps que o user autenticado pode consultar.
// Regras:
//   - super-admin: sem escopo (nil = ver tudo, cross-company).
//   - admin de company: SEMPRE todos os apps da própria company.
//   - editor/viewer: se tem grant explícito em user_app_permissions,
//     restringe àqueles N apps; sem grant, vê todos os apps da company
//     (default útil para onboarding — admin depois restringe se quiser).
//
// Se resulta em zero apps (ex.: company sem apps cadastrados), injetamos
// uma sentinela impossível — queries devolvem zero resultados sem precisar
// de branch especial no repo. Usar SEMPRE depois de bearerAuth.
func tenantScope(apps domain.AppStore, perms domain.UserAppPermissionStore, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, ok := IdentityFrom(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			if identity.IsSuper {
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), appScopeKey, []string(nil))))
				return
			}

			var (
				names []string
				err   error
			)
			switch identity.Role {
			case domain.RoleAdmin:
				names, err = apps.ListAppNamesByCompany(r.Context(), identity.CompanyID)
			case domain.RoleEditor, domain.RoleViewer:
				// Tenta grant explícito primeiro. Se não houver, cai pro
				// default útil: todos os apps da company.
				names, err = perms.ListAppNamesForUser(r.Context(), identity.UserID)
				if err == nil && len(names) == 0 {
					names, err = apps.ListAppNamesByCompany(r.Context(), identity.CompanyID)
				}
			default:
				// Role futura sem tratamento explícito: nega por segurança.
				names = nil
			}
			if err != nil {
				logger.WarnContext(r.Context(), "escopo de tenant falhou — negando queries", "err", err)
				writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "escopo de tenant indisponível"})
				return
			}
			if len(names) == 0 {
				names = []string{"__ndovu_no_apps__"}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), appScopeKey, names)))
		})
	}
}

// requireRole restringe o grupo a um papel (usar após bearerAuth).
func requireRole(role domain.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, ok := IdentityFrom(r.Context())
			if !ok || identity.Role != role {
				writeJSON(w, http.StatusForbidden, errorResponse{Error: "permissão insuficiente"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireAnyRole restringe o grupo a qualquer uma das roles listadas
// (usar após bearerAuth). Usado por rotas de escrita colaborativa
// (funnels, triagem de issues) que aceitam editor e admin, mas não viewer.
func requireAnyRole(roles ...domain.Role) func(http.Handler) http.Handler {
	allowed := map[domain.Role]bool{}
	for _, role := range roles {
		allowed[role] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, ok := IdentityFrom(r.Context())
			if !ok || !allowed[identity.Role] {
				writeJSON(w, http.StatusForbidden, errorResponse{Error: "permissão insuficiente"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// cors libera as origens configuradas (dashboard e SDKs em browser).
func cors(origins string) func(http.Handler) http.Handler {
	allowAll := origins == "*" || origins == ""
	allowed := map[string]bool{}
	for _, o := range strings.Split(origins, ",") {
		allowed[strings.TrimSpace(o)] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && (allowAll || allowed[origin]) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Api-Key, Authorization")
				w.Header().Set("Access-Control-Max-Age", "600")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requestLogger loga cada request com latência e status. Também alimenta as
// métricas Prometheus (contador por método/path/status + histograma de latência).
func requestLogger(logger *slog.Logger, metrics *platform.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			duration := time.Since(start)
			logger.Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", duration.Milliseconds(),
			)
			if metrics != nil {
				labels := map[string]string{
					"method": r.Method,
					"path":   routeTemplate(r),
					"status": fmt.Sprintf("%d", sw.status),
				}
				metrics.Counter("ndovu_http_requests_total", "Total HTTP requests", labels, 1)
				metrics.ObserveSince("ndovu_http_request_duration_seconds", "HTTP request latency", labels, start)
			}
		})
	}
}

// routeTemplate devolve o padrão casado pelo chi (ex.: /v1/events/{id})
// em vez do path cru — evita cardinality explosion no Prometheus.
func routeTemplate(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if pattern := rc.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return r.URL.Path
}

// recoverer evita que um panic derrube o servidor.
func recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recuperado", "panic", rec, "path", r.URL.Path)
					writeJSON(w, http.StatusInternalServerError,
						errorResponse{Error: "erro interno"})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// maxBody limita o tamanho do corpo aceito.
func maxBody(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
