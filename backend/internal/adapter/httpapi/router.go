package httpapi

import (
	"embed"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/marcoscorrea/ndovu/backend/internal/config"
	"github.com/marcoscorrea/ndovu/backend/internal/domain"
	"github.com/marcoscorrea/ndovu/backend/internal/platform"
	"github.com/marcoscorrea/ndovu/backend/internal/usecase"
)


// NewRouter monta todas as rotas da API.
//
// Ingestão: POST /v1/events (X-Api-Key gerida no control plane) — única escrita de traces.
// Consulta: GET  /v1/*      — exige Bearer token; read-only por construção.
// Admin:    /v1/auth/* e /v1/admin/* — gestão de usuários e chaves (role admin).
func NewRouter(h *Handlers, cfg config.Config, verifier domain.TokenVerifier, keys *usecase.APIKeyService, apps domain.AppStore, perms domain.UserAppPermissionStore, metrics *platform.Metrics, logger *slog.Logger, openapi embed.FS) http.Handler {
	r := chi.NewRouter()

	r.Use(recoverer(logger))
	r.Use(requestLogger(logger, metrics))
	r.Use(cors(cfg.CORSOrigins))
	r.Use(maxBody(cfg.MaxBodyBytes))

	r.Get("/health", h.Health)
	r.Get("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		if metrics != nil {
			_ = metrics.Render(w)
		}
	})

	// Documentação: OpenAPI escrito à mão + Swagger UI (CDN).
	r.Get("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		data, err := openapi.ReadFile("openapi.yaml")
		if err != nil {
			http.Error(w, "spec indisponível", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		_, _ = w.Write(data)
	})
	r.Get("/docs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(swaggerHTML))
	})

	r.Route("/v1", func(r chi.Router) {
		// Autenticação do backoffice
		r.Post("/auth/login", h.PostLogin)
		r.Group(func(r chi.Router) {
			r.Use(bearerAuth(verifier))
			r.Get("/auth/me", h.GetMe)
		})

		// Ingestão (única escrita de traces) — chave validada no control plane
		r.Group(func(r chi.Router) {
			r.Use(apiKeyAuth(keys, metrics))
			r.Post("/events", h.PostEvents)
			// Snapshots do SDK (session replay MVP) usam a mesma X-Api-Key.
			r.Post("/snapshots", h.PostSnapshot)
			// Feedback do widget SDK — mesma auth.
			r.Post("/feedbacks", h.PostFeedback)
		})

		// Consulta e operações do dashboard — exigem Bearer + scope de tenant.
		// Subdividido em 2 blocos por permissão:
		//   (a) leitura + saved views próprias : viewer + editor + admin
		//   (b) escrita colaborativa (triagem + funnels) : editor + admin
		r.Group(func(r chi.Router) {
			r.Use(bearerAuth(verifier))
			r.Use(tenantScope(apps, perms, logger))

			// (a) Leitura e ações permitidas ao viewer
			r.Get("/events", h.GetEvents)
			r.Get("/events/{id}", h.GetEvent)
			r.Get("/events/{id}/resolved-stack", h.GetResolvedStack)
			r.Get("/snapshots/event/{eventId}", h.GetSnapshotMeta)
			r.Get("/snapshots/event/{eventId}/html", h.GetSnapshot)
			r.Get("/sessions", h.GetSessions)
			r.Get("/sessions/{sessionId}", h.GetSession)
			r.Get("/traces/{traceId}", h.GetTrace)
			r.Get("/stats/overview", h.GetOverview)
			r.Get("/stats/vitals", h.GetWebVitals)
			r.Get("/stats/compare", h.GetReleaseCompare)
			r.Get("/stats/retention", h.GetRetention)
			r.Get("/issues", h.GetIssues)
			r.Get("/issues/{fingerprint}/comments", h.GetIssueComments)
			r.Get("/releases", h.GetReleases)
			r.Get("/meta/filters", h.GetFilterOptions)
			r.Get("/funnels", h.GetFunnels)
			r.Get("/funnels/{id}/results", h.GetFunnelResults)
			// Saved views: viewer pode criar e gerir as próprias. A regra
			// de "compartilhar" (isShared=true) é validada no usecase e
			// bloqueia viewer.
			r.Get("/saved-views", h.GetSavedViews)
			r.Post("/saved-views", h.PostSavedView)
			r.Patch("/saved-views/{id}", h.PatchSavedView)
			r.Delete("/saved-views/{id}", h.DeleteSavedView)

			// (b) Escrita colaborativa — só editor ou admin.
			r.Group(func(r chi.Router) {
				r.Use(requireAnyRole(domain.RoleEditor, domain.RoleAdmin))
				r.Patch("/issues/{fingerprint}", h.PatchIssue)
				r.Post("/issues/{fingerprint}/comments", h.PostIssueComment)
				r.Delete("/issues/comments/{id}", h.DeleteIssueComment)
				r.Post("/funnels", h.PostFunnel)
				r.Patch("/funnels/{id}", h.PatchFunnel)
				r.Delete("/funnels/{id}", h.DeleteFunnel)
			})
		})

		// Admin — gestão de usuários, apps e chaves (role admin)
		r.Route("/admin", func(r chi.Router) {
			r.Use(bearerAuth(verifier))
			r.Use(requireRole(domain.RoleAdmin))
			r.Get("/users", h.GetUsers)
			r.Post("/users", h.PostUsers)
			r.Patch("/users/{id}", h.PatchUser)
			r.Get("/users/{id}/permissions", h.GetUserPermissions)
			r.Put("/users/{id}/permissions/{appId}", h.PutUserPermission)
			r.Delete("/users/{id}/permissions/{appId}", h.DeleteUserPermission)
			r.Get("/apps", h.GetApps)
			r.Post("/apps", h.PostApps)
			r.Get("/apps/{id}", h.GetApp)
			r.Patch("/apps/{id}", h.PatchApp)
			r.Delete("/apps/{id}", h.DeleteApp)
			r.Get("/companies", h.GetCompanies)
			r.Post("/companies", h.PostCompanies)
			r.Get("/companies/{id}", h.GetCompany)
			r.Patch("/companies/{id}", h.PatchCompany)
			r.Delete("/companies/{id}", h.DeleteCompany)
			r.Get("/api-keys", h.GetAPIKeys)
			r.Post("/api-keys", h.PostAPIKeys)
			r.Delete("/api-keys/{id}", h.DeleteAPIKey)
			r.Get("/alerts", h.GetAlerts)
			r.Post("/alerts", h.PostAlerts)
			r.Delete("/alerts/{id}", h.DeleteAlert)
			r.Get("/source-maps", h.GetSourceMaps)
			r.Post("/source-maps", h.PostSourceMaps)
			r.Delete("/source-maps/{id}", h.DeleteSourceMap)
			r.Post("/digest/send-now", h.PostDigestSendNow)
			r.Get("/audit-log", h.GetAuditLog)
			r.Get("/sampling-rules", h.GetSamplingRules)
			r.Post("/sampling-rules", h.PostSamplingRule)
			r.Patch("/sampling-rules/{id}", h.PatchSamplingRule)
			r.Delete("/sampling-rules/{id}", h.DeleteSamplingRule)
			r.Get("/anomaly-rules", h.GetAnomalyRules)
			r.Post("/anomaly-rules", h.PostAnomalyRule)
			r.Patch("/anomaly-rules/{id}", h.PatchAnomalyRule)
			r.Delete("/anomaly-rules/{id}", h.DeleteAnomalyRule)
			r.Get("/anomaly-detections", h.GetAnomalyDetections)
			r.Get("/feedbacks", h.GetFeedbacks)
			r.Patch("/feedbacks/{id}", h.PatchFeedback)
			r.Delete("/feedbacks/{id}", h.DeleteFeedback)
			r.Get("/gdpr/user/{userId}/export", h.GetGDPRExport)
			r.Delete("/gdpr/user/{userId}", h.DeleteGDPRUser)
		})
	})

	return r
}

const swaggerHTML = `<!DOCTYPE html>
<html lang="pt-BR">
<head>
  <meta charset="utf-8"/>
  <title>Ndovu API — Docs</title>
  <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/swagger-ui/5.11.0/swagger-ui.min.css"/>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdnjs.cloudflare.com/ajax/libs/swagger-ui/5.11.0/swagger-ui-bundle.min.js"></script>
  <script>
    window.onload = () => {
      SwaggerUIBundle({ url: '/openapi.yaml', dom_id: '#swagger-ui' });
    };
  </script>
</body>
</html>`
