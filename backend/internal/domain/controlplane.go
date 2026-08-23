package domain

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Erros do control plane.
var (
	// ErrUnauthorized indica credenciais/token inválidos.
	ErrUnauthorized = errors.New("não autorizado")
	// ErrForbidden indica permissão insuficiente (role).
	ErrForbidden = errors.New("permissão insuficiente")
	// ErrConflict indica violação de unicidade (ex.: e-mail já cadastrado).
	ErrConflict = errors.New("recurso já existe")
)

// Role define o papel de um usuário do backoffice.
type Role string

const (
	// RoleAdmin gerencia tudo: usuários, empresas, apps, chaves, alertas,
	// sampling, source maps, LGPD e o bloco de admin em geral. Também pode
	// tudo que editor e viewer fazem.
	RoleAdmin Role = "admin"
	// RoleEditor pode ver + operar: triagem de issues (mudar status +
	// assignee + comentar), criar/editar/deletar funnels e saved views
	// compartilhadas. NÃO acessa o bloco de admin.
	RoleEditor Role = "editor"
	// RoleViewer é leitura pura. Vê traces, sessions, overview, issues
	// (sem alterar), funnels (sem criar/editar), releases. Pode criar
	// saved views próprias (privadas), mas não compartilhá-las.
	RoleViewer Role = "viewer"
)

// Valid informa se o papel é reconhecido.
func (r Role) Valid() bool {
	return r == RoleAdmin || r == RoleEditor || r == RoleViewer
}

// AtLeastEditor devolve true se a role tem privilégio de editor ou mais
// (editor ou admin). Usado para autorizar operações de escrita colaborativa.
func (r Role) AtLeastEditor() bool { return r == RoleAdmin || r == RoleEditor }

// User é um usuário do backoffice (a ferramenta é multiusuário e
// auto gerenciável: admins criam e administram contas). Todo usuário
// pertence a uma empresa; super-admin transcende empresa (vê tudo).
type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      Role      `json:"role"`
	Active    bool      `json:"active"`
	IsSuper   bool      `json:"isSuper"`
	CompanyID string    `json:"companyId"`
	Company   string    `json:"company,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Company é uma empresa cliente da ferramenta. Usuários pertencem a uma
// empresa; apps podem opcionalmente ser atrelados a uma.
type Company struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Document  string    `json:"document,omitempty"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// CompanyStore é o port de persistência de empresas.
type CompanyStore interface {
	CreateCompany(ctx context.Context, c Company) (Company, error)
	GetCompany(ctx context.Context, id string) (Company, error)
	ListCompanies(ctx context.Context) ([]Company, error)
	UpdateCompany(ctx context.Context, id string, c Company) (Company, error)
	DeleteCompany(ctx context.Context, id string) error
}

// APIKey identifica um app emissor na ingestão. A chave em claro só existe
// no momento da criação; armazenamos apenas o hash e um prefixo de exibição.
type APIKey struct {
	ID        string     `json:"id"`
	AppID     string     `json:"appId,omitempty"`
	App       string     `json:"app"`
	Label     string     `json:"label,omitempty"`
	Prefix    string     `json:"prefix"`
	Active    bool       `json:"active"`
	CreatedAt time.Time  `json:"createdAt"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
}

// App é um frontend emissor cadastrado no control plane. O cadastro guarda os
// metadados de contato/integração e gera a primeira chave de API do app.
// CompanyID é opcional (nem todo app está atrelado a uma empresa cadastrada);
// Company mantém o valor exibível resolvido a partir do FK.
type App struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Technology  string    `json:"technology,omitempty"`
	CompanyID   string    `json:"companyId,omitempty"`
	Company     string    `json:"company,omitempty"`
	Responsible string    `json:"responsible,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Identity é o resultado da verificação de um token: quem está chamando.
// É o contrato que um provedor externo (ex.: Keycloak/OIDC) também atende.
// CompanyID vira o escopo de multi-tenancy: queries filtram por apps dessa
// company (exceto se IsSuper=true, aí vê tudo).
type Identity struct {
	UserID    string
	Email     string
	Name      string
	Role      Role
	CompanyID string
	IsSuper   bool
}

// TokenVerifier valida um bearer token e devolve a identidade.
// Hoje: JWT próprio (HS256). Amanhã: um adapter OIDC/Keycloak implementa
// esta mesma interface e o resto da aplicação não muda.
type TokenVerifier interface {
	Verify(ctx context.Context, token string) (Identity, error)
}

// UserStore é o port de persistência de usuários.
type UserStore interface {
	CreateUser(ctx context.Context, u User, passwordHash string) (User, error)
	GetUserByEmail(ctx context.Context, email string) (User, string, error) // user, hash
	GetUserByID(ctx context.Context, id string) (User, error)
	ListUsers(ctx context.Context) ([]User, error)
	UpdateUser(ctx context.Context, id string, role *Role, active *bool, name *string, companyID *string) (User, error)
	SetPassword(ctx context.Context, id string, passwordHash string) error
	CountActiveAdmins(ctx context.Context) (int, error)
}

// APIKeyStore é o port de persistência de chaves de ingestão.
type APIKeyStore interface {
	CreateAPIKey(ctx context.Context, k APIKey, keyHash string, createdBy string) (APIKey, error)
	ListAPIKeys(ctx context.Context) ([]APIKey, error)
	RevokeAPIKey(ctx context.Context, id string) error
	FindActiveKeyByHash(ctx context.Context, keyHash string) (APIKey, error)
}

// AppStore é o port de persistência de apps emissores (frontends cadastrados).
type AppStore interface {
	CreateApp(ctx context.Context, a App) (App, error)
	GetApp(ctx context.Context, id string) (App, error)
	ListApps(ctx context.Context) ([]App, error)
	UpdateApp(ctx context.Context, id string, a App) (App, error)
	DeleteApp(ctx context.Context, id string) error
	// ListAppNamesByCompany devolve os nomes dos apps de uma empresa —
	// usado para montar o AppScope de queries multi-tenant.
	ListAppNamesByCompany(ctx context.Context, companyID string) ([]string, error)
}

// FeedbackType classifica o tipo de mensagem que o usuário final enviou.
type FeedbackType string

const (
	FeedbackBug        FeedbackType = "bug"
	FeedbackSuggestion FeedbackType = "suggestion"
	FeedbackPraise     FeedbackType = "praise"
	FeedbackOther      FeedbackType = "other"
)

// Valid indica se o tipo é reconhecido.
func (t FeedbackType) Valid() bool {
	switch t {
	case FeedbackBug, FeedbackSuggestion, FeedbackPraise, FeedbackOther:
		return true
	}
	return false
}

// FeedbackStatus é o estado de triagem no backoffice.
type FeedbackStatus string

const (
	FeedbackNew       FeedbackStatus = "new"
	FeedbackTriaging  FeedbackStatus = "triaging"
	FeedbackResolved  FeedbackStatus = "resolved"
	FeedbackDismissed FeedbackStatus = "dismissed"
)

// Valid indica se o status é reconhecido.
func (s FeedbackStatus) Valid() bool {
	switch s {
	case FeedbackNew, FeedbackTriaging, FeedbackResolved, FeedbackDismissed:
		return true
	}
	return false
}

// UserFeedback é uma reclamação/sugestão do usuário final capturada pelo
// widget do SDK. Vive por session_id (sempre); event_id é o último trace em
// memória quando o widget foi aberto (opcional — pode não haver erro).
type UserFeedback struct {
	ID          string         `json:"id"`
	App         string         `json:"app"`
	SessionID   string         `json:"sessionId"`
	EventID     string         `json:"eventId,omitempty"`
	UserID      string         `json:"userId,omitempty"`
	Type        FeedbackType   `json:"type"`
	Message     string         `json:"message"`
	Email       string         `json:"email,omitempty"`
	URL         string         `json:"url,omitempty"`
	ViewportW   int            `json:"viewportW,omitempty"`
	ViewportH   int            `json:"viewportH,omitempty"`
	Status      FeedbackStatus `json:"status"`
	ResolvedBy  string         `json:"resolvedBy,omitempty"`
	ResolvedAt  *time.Time     `json:"resolvedAt,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
}

// FeedbackFilter para consulta paginada no backoffice.
type FeedbackFilter struct {
	App       string
	Status    string
	SessionID string // usado para materializar Session quando o ClickHouse não tem eventos
	Limit     int
	Offset    int
}

// FeedbackStore persiste feedbacks.
type FeedbackStore interface {
	CreateFeedback(ctx context.Context, f UserFeedback) (UserFeedback, error)
	UpdateFeedbackStatus(ctx context.Context, id string, status FeedbackStatus, resolvedBy string) (UserFeedback, error)
	ListFeedbacks(ctx context.Context, f FeedbackFilter) ([]UserFeedback, int, error)
	DeleteFeedback(ctx context.Context, id string) error
}

// AnomalyMetric enumera as métricas suportadas para detecção.
type AnomalyMetric string

const (
	AnomalyErrorCount AnomalyMetric = "error_count"
	AnomalyEventCount AnomalyMetric = "event_count"
	AnomalyErrorRate  AnomalyMetric = "error_rate"
)

// Valid indica se a métrica é reconhecida.
func (m AnomalyMetric) Valid() bool {
	switch m {
	case AnomalyErrorCount, AnomalyEventCount, AnomalyErrorRate:
		return true
	}
	return false
}

// AnomalyDirection define o lado da distribuição que dispara.
type AnomalyDirection string

const (
	AnomalyAbove AnomalyDirection = "above" // spike (z-score positivo)
	AnomalyBelow AnomalyDirection = "below" // silêncio (z-score negativo)
	AnomalyBoth  AnomalyDirection = "both"
)

// Valid indica se a direção é reconhecida.
func (d AnomalyDirection) Valid() bool {
	return d == AnomalyAbove || d == AnomalyBelow || d == AnomalyBoth
}

// AnomalyRule descreve uma regra de detecção. Persistida no control plane;
// avaliada periodicamente pelo writer.
type AnomalyRule struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	App            string           `json:"app,omitempty"`
	Metric         AnomalyMetric    `json:"metric"`
	WindowMinutes  int              `json:"windowMinutes"`
	BaselineWeeks  int              `json:"baselineWeeks"`
	Sensitivity    float64          `json:"sensitivity"`
	Direction      AnomalyDirection `json:"direction"`
	SilenceSeconds int              `json:"silenceSeconds"`
	Channel        AlertChannel     `json:"channel"`
	TargetURL      string           `json:"targetUrl"`
	Active         bool             `json:"active"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
}

// AnomalyDetection é o registro de um disparo.
type AnomalyDetection struct {
	ID             string           `json:"id"`
	RuleID         string           `json:"ruleId"`
	RuleName       string           `json:"ruleName,omitempty"` // resolvido via join
	DetectedAt     time.Time        `json:"detectedAt"`
	CurrentValue   float64          `json:"currentValue"`
	BaselineAvg    float64          `json:"baselineAvg"`
	BaselineStddev float64          `json:"baselineStddev"`
	ZScore         float64          `json:"zScore"`
	Direction      AnomalyDirection `json:"direction"`
	NotifyOK       bool             `json:"notifyOk"`
	NotifyDetail   string           `json:"notifyDetail,omitempty"`
}

// AnomalyStore persiste regras e histórico de detecções.
type AnomalyStore interface {
	CreateAnomalyRule(ctx context.Context, r AnomalyRule) (AnomalyRule, error)
	UpdateAnomalyRule(ctx context.Context, id string, r AnomalyRule) (AnomalyRule, error)
	DeleteAnomalyRule(ctx context.Context, id string) error
	ListAnomalyRules(ctx context.Context) ([]AnomalyRule, error)
	LastDetection(ctx context.Context, ruleID string) (AnomalyDetection, error)
	RecordDetection(ctx context.Context, d AnomalyDetection) error
	ListDetections(ctx context.Context, limit, offset int) ([]AnomalyDetection, int, error)
}

// SessionSnapshot é a metadata de um snapshot HTML capturado pelo SDK no
// momento de um error(). O payload (HTML gzip'ado) vive em blob storage
// (MinIO/S3); Postgres guarda só a referência via ObjectKey.
type SessionSnapshot struct {
	ID         string    `json:"id"`
	EventID    string    `json:"eventId"`
	SessionID  string    `json:"sessionId"`
	App        string    `json:"app"`
	ObjectKey  string    `json:"objectKey"`
	SizeBytes  int       `json:"sizeBytes"`
	ViewportW  int       `json:"viewportW,omitempty"`
	ViewportH  int       `json:"viewportH,omitempty"`
	URL        string    `json:"url,omitempty"`
	TakenAt    time.Time `json:"takenAt"`
	ReceivedAt time.Time `json:"receivedAt"`
}

// SnapshotMetaStore persiste a metadata (Postgres). O payload vai por outro
// caminho (SnapshotBlobStore) — separação permite trocar S3 por outra
// coisa sem tocar no Postgres, e vice-versa.
type SnapshotMetaStore interface {
	CreateSnapshotMeta(ctx context.Context, s SessionSnapshot) error
	GetSnapshotMetaByEvent(ctx context.Context, eventID string) (SessionSnapshot, error)
}

// SnapshotBlobStore guarda o HTML compactado. MinIO/S3 hoje; local/disco
// funcionaria com a mesma interface.
type SnapshotBlobStore interface {
	PutSnapshot(ctx context.Context, key string, content []byte) error
	GetSnapshot(ctx context.Context, key string) ([]byte, error)
}

// SamplingRule descarta uma fração de eventos de (app, event_type) antes de
// gravar no ClickHouse. Precedência: mais específica ganha. Erros passam
// automaticamente quando KeepErrors=true (default), evitando perder sinais
// críticos ao amortizar volume de page_view/action.
type SamplingRule struct {
	ID         string    `json:"id"`
	App        string    `json:"app"`       // "" = todos os apps
	EventType  string    `json:"eventType"` // "" = todos os tipos
	SampleRate float64   `json:"sampleRate"`
	KeepErrors bool      `json:"keepErrors"`
	Active     bool      `json:"active"`
	Note       string    `json:"note,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// SamplingRuleStore persiste as regras e permite refresh in-memory no writer.
type SamplingRuleStore interface {
	CreateSamplingRule(ctx context.Context, r SamplingRule) (SamplingRule, error)
	UpdateSamplingRule(ctx context.Context, id string, r SamplingRule) (SamplingRule, error)
	DeleteSamplingRule(ctx context.Context, id string) error
	ListSamplingRules(ctx context.Context) ([]SamplingRule, error)
}

// UserAppPermission concede acesso explícito de um user a um app (Sprint E.4).
// Só faz sentido para viewers — admins da company já veem tudo da company e
// super-admins veem tudo global.
type UserAppPermission struct {
	UserID    string    `json:"userId"`
	AppID     string    `json:"appId"`
	AppName   string    `json:"appName,omitempty"`
	Role      string    `json:"role"` // "viewer" | "editor" (editor reservado para futuro)
	GrantedAt time.Time `json:"grantedAt"`
}

// UserAppPermissionStore persiste as concessões e permite consulta pelo
// tenantScope middleware.
type UserAppPermissionStore interface {
	GrantAppPermission(ctx context.Context, userID, appID, role, grantedBy string) (UserAppPermission, error)
	RevokeAppPermission(ctx context.Context, userID, appID string) error
	ListAppPermissionsForUser(ctx context.Context, userID string) ([]UserAppPermission, error)
	// ListAppNamesForUser devolve os nomes dos apps que o user tem permissão
	// explícita — usado para montar o AppScope de viewers. Deriva de
	// user_app_permissions JOIN apps.
	ListAppNamesForUser(ctx context.Context, userID string) ([]string, error)
}

// AuditEntry é um registro imutável de ação admin. Guardado para revisão
// de segurança/compliance (quem fez o quê, quando, com qual IP).
type AuditEntry struct {
	ID           string          `json:"id"`
	ActorUserID  string          `json:"actorUserId,omitempty"`
	ActorEmail   string          `json:"actorEmail"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resourceType,omitempty"`
	ResourceID   string          `json:"resourceId,omitempty"`
	Details      json.RawMessage `json:"details,omitempty"`
	IP           string          `json:"ip,omitempty"`
	UserAgent    string          `json:"userAgent,omitempty"`
	CreatedAt    time.Time       `json:"createdAt"`
}

// AuditFilter para consulta paginada.
type AuditFilter struct {
	Actor        string // user_id
	Action       string
	ResourceType string
	From         *time.Time
	To           *time.Time
	Limit        int
	Offset       int
}

// AuditStore persiste eventos de auditoria (insert-only) e permite consulta
// paginada. Não expõe update/delete — auditoria não deve ser reescrita.
type AuditStore interface {
	RecordAudit(ctx context.Context, e AuditEntry) error
	ListAudit(ctx context.Context, f AuditFilter) ([]AuditEntry, int, error)
}

// Funnel é uma definição persistida de funil (nome + steps).
// A execução (contagem por step) é feita on-demand no ClickHouse.
type Funnel struct {
	ID            string          `json:"id"`
	App           string          `json:"app"`
	Name          string          `json:"name"`
	WindowSeconds int             `json:"windowSeconds"`
	Steps         json.RawMessage `json:"steps"` // []FunnelStep serializado
	CreatedBy     string          `json:"createdBy,omitempty"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}

// FunnelStore persiste definições de funil.
type FunnelStore interface {
	CreateFunnel(ctx context.Context, f Funnel) (Funnel, error)
	UpdateFunnel(ctx context.Context, id string, name string, windowSeconds int, steps json.RawMessage) (Funnel, error)
	ListFunnels(ctx context.Context, app string) ([]Funnel, error)
	GetFunnel(ctx context.Context, id string) (Funnel, error)
	DeleteFunnel(ctx context.Context, id string) error
}

// SavedView é um preset de filtros salvo por um usuário do backoffice.
// Vive por (owner, view_type, name) — dois usuários podem ter views com o
// mesmo nome, mas o mesmo usuário não repete nome dentro de um tipo.
// Filters é opaco (JSON) — cada tela decide o formato dos filtros que salva.
type SavedView struct {
	ID          string          `json:"id"`
	OwnerUserID string          `json:"ownerUserId"`
	OwnerName   string          `json:"ownerName,omitempty"`
	ViewType    string          `json:"viewType"`
	Name        string          `json:"name"`
	Filters     json.RawMessage `json:"filters"`
	IsShared    bool            `json:"isShared"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

// SavedViewStore persiste as views. Toda listagem já filtra por escopo
// (do usuário atual + as compartilhadas por outros).
type SavedViewStore interface {
	CreateSavedView(ctx context.Context, v SavedView) (SavedView, error)
	UpdateSavedView(ctx context.Context, id, ownerID string, name string, filters json.RawMessage, isShared bool) (SavedView, error)
	DeleteSavedView(ctx context.Context, id, ownerID string) error
	ListSavedViews(ctx context.Context, ownerID, viewType string) ([]SavedView, error)
}

// SourceMap é um .map JS associado a (app, release, arquivo). O conteúdo
// costuma ser grande — o dashboard só lista metadata; o binário só é lido
// pelo serviço de resolução de stack.
type SourceMap struct {
	ID         string    `json:"id"`
	App        string    `json:"app"`
	Release    string    `json:"release"`
	Filename   string    `json:"filename"`
	SizeBytes  int       `json:"sizeBytes"`
	UploadedBy string    `json:"uploadedBy,omitempty"`
	UploadedAt time.Time `json:"uploadedAt"`
}

// SourceMapStore persiste source maps por (app, release, filename).
// Upsert por chave permite reenvio do mesmo arquivo após rebuild.
type SourceMapStore interface {
	UpsertSourceMap(ctx context.Context, m SourceMap, content string) (SourceMap, error)
	ListSourceMaps(ctx context.Context, app, release string) ([]SourceMap, error)
	GetSourceMapContent(ctx context.Context, app, release, filename string) (string, error)
	DeleteSourceMap(ctx context.Context, id string) error
}

// AlertChannel indica o destino da notificação de um alerta.
type AlertChannel string

const (
	AlertChannelSlack   AlertChannel = "slack"
	AlertChannelWebhook AlertChannel = "webhook"
)

// Valid indica se o canal é reconhecido.
func (c AlertChannel) Valid() bool { return c == AlertChannelSlack || c == AlertChannelWebhook }

// AlertRule é uma regra "se X erros em Y segundos, notifica Z".
// A avaliação roda periodicamente no processo do writer.
type AlertRule struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	App            string       `json:"app,omitempty"`
	ErrorCode      string       `json:"errorCode,omitempty"`
	Threshold      int          `json:"threshold"`
	WindowSeconds  int          `json:"windowSeconds"`
	Channel        AlertChannel `json:"channel"`
	TargetURL      string       `json:"targetUrl"`
	SilenceSeconds int          `json:"silenceSeconds"`
	Active         bool         `json:"active"`
	CreatedAt      time.Time    `json:"createdAt"`
	UpdatedAt      time.Time    `json:"updatedAt"`
}

// AlertRuleStore persiste regras de alerta e histórico de disparos.
type AlertRuleStore interface {
	CreateAlertRule(ctx context.Context, rule AlertRule) (AlertRule, error)
	ListAlertRules(ctx context.Context) ([]AlertRule, error)
	DeleteAlertRule(ctx context.Context, id string) error
	LastDeliveryAt(ctx context.Context, ruleID string) (time.Time, error)
	RecordDelivery(ctx context.Context, ruleID string, count int, ok bool, detail string) error
}
