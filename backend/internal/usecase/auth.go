package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// AuthService autentica usuários do backoffice e emite/verifica tokens.
//
// A emissão é local (JWT HS256), mas a VERIFICAÇÃO fica atrás do port
// domain.TokenVerifier: quando a empresa plugar Keycloak/OIDC, um adapter
// que valida tokens do IdP substitui este serviço sem tocar em mais nada.
type AuthService struct {
	users     domain.UserStore
	companies domain.CompanyStore
	secret    []byte
	tokenTTL  time.Duration
	logger    *slog.Logger
	now       func() time.Time
}

// NewAuthService cria o serviço de autenticação local.
func NewAuthService(users domain.UserStore, companies domain.CompanyStore, secret string, tokenTTL time.Duration, logger *slog.Logger) *AuthService {
	return &AuthService{
		users:     users,
		companies: companies,
		secret:    []byte(secret),
		tokenTTL:  tokenTTL,
		logger:    logger,
		now:       time.Now,
	}
}

// LoginResult carrega o token emitido e o usuário autenticado.
type LoginResult struct {
	Token     string      `json:"token"`
	ExpiresAt time.Time   `json:"expiresAt"`
	User      domain.User `json:"user"`
}

// Login verifica e-mail/senha e emite um JWT.
func (s *AuthService) Login(ctx context.Context, email, password string) (LoginResult, error) {
	if email == "" || password == "" {
		return LoginResult{}, domain.NewValidationError("email e password são obrigatórios")
	}
	user, hash, err := s.users.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			// Mesmo custo de resposta para usuário inexistente e senha errada.
			_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$invalidinvalidinvalidinvalidinvalidinvalid"), []byte(password))
			return LoginResult{}, domain.ErrUnauthorized
		}
		return LoginResult{}, fmt.Errorf("consultando usuário: %w", err)
	}
	if !user.Active {
		return LoginResult{}, domain.ErrUnauthorized
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return LoginResult{}, domain.ErrUnauthorized
	}

	expires := s.now().Add(s.tokenTTL)
	claims := jwt.MapClaims{
		"sub":       user.ID,
		"email":     user.Email,
		"name":      user.Name,
		"role":      string(user.Role),
		"companyId": user.CompanyID,
		"isSuper":   user.IsSuper,
		"iat":       s.now().Unix(),
		"exp":       expires.Unix(),
		"iss":       "ndovu",
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return LoginResult{}, fmt.Errorf("assinando token: %w", err)
	}

	s.logger.InfoContext(ctx, "login", "user", user.Email, "role", user.Role)
	return LoginResult{Token: token, ExpiresAt: expires, User: user}, nil
}

// Verify implementa domain.TokenVerifier para o JWT local.
func (s *AuthService) Verify(_ context.Context, tokenStr string) (domain.Identity, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("método de assinatura inesperado: %v", t.Header["alg"])
		}
		return s.secret, nil
	}, jwt.WithIssuer("ndovu"), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return domain.Identity{}, domain.ErrUnauthorized
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return domain.Identity{}, domain.ErrUnauthorized
	}
	id := domain.Identity{
		UserID:    str(claims["sub"]),
		Email:     str(claims["email"]),
		Name:      str(claims["name"]),
		Role:      domain.Role(str(claims["role"])),
		CompanyID: str(claims["companyId"]),
	}
	// bool aparece como bool no MapClaims — só coerção defensiva.
	if v, ok := claims["isSuper"].(bool); ok {
		id.IsSuper = v
	}
	if id.UserID == "" || !id.Role.Valid() {
		return domain.Identity{}, domain.ErrUnauthorized
	}
	return id, nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// ---------------------------------------------------------------------------
// Gestão de usuários (auto gerenciável: admins administram contas)
// ---------------------------------------------------------------------------

// CreateUserInput são os dados para criar um usuário.
type CreateUserInput struct {
	Email     string
	Name      string
	Password  string
	Role      domain.Role
	CompanyID string
}

// CreateUser cria uma conta (só admins chegam aqui — o middleware garante).
func (s *AuthService) CreateUser(ctx context.Context, in CreateUserInput) (domain.User, error) {
	var issues []string
	if in.Email == "" {
		issues = append(issues, "email é obrigatório")
	}
	if in.Name == "" {
		issues = append(issues, "name é obrigatório")
	}
	if len(in.Password) < 8 {
		issues = append(issues, "password deve ter pelo menos 8 caracteres")
	}
	if !in.Role.Valid() {
		issues = append(issues, "role deve ser 'admin' ou 'viewer'")
	}
	if in.CompanyID == "" {
		issues = append(issues, "companyId é obrigatório")
	}
	if len(issues) > 0 {
		return domain.User{}, domain.NewValidationError(issues...)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return domain.User{}, fmt.Errorf("gerando hash: %w", err)
	}
	return s.users.CreateUser(ctx, domain.User{
		Email:     in.Email,
		Name:      in.Name,
		Role:      in.Role,
		Active:    true,
		CompanyID: in.CompanyID,
	}, string(hash))
}

// ListUsers lista as contas (visão global — usar só para super-admin).
func (s *AuthService) ListUsers(ctx context.Context) ([]domain.User, error) {
	return s.users.ListUsers(ctx)
}

// ListUsersByCompany lista contas de uma company específica — usar para
// admin não-super evitar vazamento cross-tenant.
func (s *AuthService) ListUsersByCompany(ctx context.Context, companyID string) ([]domain.User, error) {
	return s.users.ListUsersByCompany(ctx, companyID)
}

// GetUserByID expõe o lookup direto ao handler admin (usado para checar
// ownership em PATCH/DELETE antes de mutar).
func (s *AuthService) GetUserByID(ctx context.Context, id string) (domain.User, error) {
	return s.users.GetUserByID(ctx, id)
}

// UpdateUserInput são os campos alteráveis de uma conta.
type UpdateUserInput struct {
	Role      *domain.Role
	Active    *bool
	Name      *string
	CompanyID *string
}

// UpdateUser altera papel/ativação/nome/empresa, protegendo o último admin ativo.
func (s *AuthService) UpdateUser(ctx context.Context, id string, in UpdateUserInput) (domain.User, error) {
	if in.Role != nil && !in.Role.Valid() {
		return domain.User{}, domain.NewValidationError("role deve ser 'admin' ou 'viewer'")
	}

	// Não deixa a ferramenta ficar sem administrador.
	demoting := (in.Role != nil && *in.Role != domain.RoleAdmin) || (in.Active != nil && !*in.Active)
	if demoting {
		current, err := s.users.GetUserByID(ctx, id)
		if err != nil {
			return domain.User{}, err
		}
		if current.Role == domain.RoleAdmin && current.Active {
			admins, err := s.users.CountActiveAdmins(ctx)
			if err != nil {
				return domain.User{}, err
			}
			if admins <= 1 {
				return domain.User{}, domain.NewValidationError("não é possível remover o último admin ativo")
			}
		}
	}
	return s.users.UpdateUser(ctx, id, in.Role, in.Active, in.Name, in.CompanyID)
}

// ResetPassword define uma nova senha para a conta.
func (s *AuthService) ResetPassword(ctx context.Context, id, password string) error {
	if len(password) < 8 {
		return domain.NewValidationError("password deve ter pelo menos 8 caracteres")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("gerando hash: %w", err)
	}
	return s.users.SetPassword(ctx, id, string(hash))
}

// EnsureBootstrapAdmin cria o primeiro admin se não existir nenhum usuário —
// sem ele a ferramenta não teria como ser gerenciada no primeiro boot.
// A conta configurada em NDOVU_ADMIN_EMAIL é sempre super-admin. Isso também
// corrige contas existentes criadas antes de a migração 000010 poder promovê-las.
func (s *AuthService) EnsureBootstrapAdmin(ctx context.Context, email, password string) error {
	bootstrapUser, _, err := s.users.GetUserByEmail(ctx, email)
	if err == nil {
		if bootstrapUser.Role == domain.RoleAdmin && !bootstrapUser.IsSuper {
			if err := s.users.SetSuperAdmin(ctx, bootstrapUser.ID); err != nil {
				return fmt.Errorf("promovendo admin bootstrap: %w", err)
			}
			s.logger.Info("admin bootstrap promovido a super-admin", "email", bootstrapUser.Email)
		}
		return nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("consultando admin bootstrap: %w", err)
	}

	users, err := s.users.ListUsers(ctx)
	if err != nil {
		return fmt.Errorf("verificando usuários existentes: %w", err)
	}
	if len(users) > 0 {
		return nil
	}
	companies, err := s.companies.ListCompanies(ctx)
	if err != nil {
		return fmt.Errorf("consultando empresas: %w", err)
	}
	var defaultCompany string
	for _, c := range companies {
		if c.Name == "Padrão" || defaultCompany == "" {
			defaultCompany = c.ID
			if c.Name == "Padrão" {
				break
			}
		}
	}
	if defaultCompany == "" {
		return fmt.Errorf("nenhuma empresa cadastrada para atrelar o admin inicial")
	}
	created, err := s.CreateUser(ctx, CreateUserInput{
		Email:     email,
		Name:      "Administrador",
		Password:  password,
		Role:      domain.RoleAdmin,
		CompanyID: defaultCompany,
	})
	if err != nil {
		return fmt.Errorf("criando admin inicial: %w", err)
	}
	if err := s.users.SetSuperAdmin(ctx, created.ID); err != nil {
		return fmt.Errorf("promovendo admin inicial a super-admin: %w", err)
	}
	s.logger.Info("admin inicial criado — troque a senha no primeiro acesso", "email", email)
	return nil
}
