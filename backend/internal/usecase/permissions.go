package usecase

import (
	"context"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// PermissionService cuida do RBAC granular por app (Sprint E.4).
//   - super-admin: vê tudo, ignora esta camada.
//   - admin de company: vê todos os apps da company automaticamente; ainda
//     pode granted apps de outras companies (não fizemos esse caso — hoje
//     bloqueamos cross-company).
//   - viewer: vê SOMENTE apps explicitamente concedidos.
type PermissionService struct {
	store domain.UserAppPermissionStore
	users domain.UserStore
	apps  domain.AppStore
}

// NewPermissionService cria o serviço.
func NewPermissionService(store domain.UserAppPermissionStore, users domain.UserStore, apps domain.AppStore) *PermissionService {
	return &PermissionService{store: store, users: users, apps: apps}
}

// Grant concede acesso de user a app. Valida que ambos existem e que o app
// pertence à mesma company do user (não permitimos cross-company).
func (s *PermissionService) Grant(ctx context.Context, userID, appID, role, grantedBy string) (domain.UserAppPermission, error) {
	if userID == "" || appID == "" {
		return domain.UserAppPermission{}, domain.NewValidationError("userId e appId são obrigatórios")
	}
	user, err := s.users.GetUserByID(ctx, userID)
	if err != nil {
		return domain.UserAppPermission{}, err
	}
	app, err := s.apps.GetApp(ctx, appID)
	if err != nil {
		return domain.UserAppPermission{}, err
	}
	// Cross-company block: app precisa ser da mesma company do user. Se o app
	// não tem company_id (legado), bloqueia — evita acesso não intencional.
	if app.CompanyID == "" || app.CompanyID != user.CompanyID {
		return domain.UserAppPermission{}, domain.NewValidationError(
			"app não pertence à mesma empresa do usuário")
	}
	return s.store.GrantAppPermission(ctx, userID, appID, role, grantedBy)
}

// Revoke remove uma permissão. 404 se não existir.
func (s *PermissionService) Revoke(ctx context.Context, userID, appID string) error {
	return s.store.RevokeAppPermission(ctx, userID, appID)
}

// ListForUser lista as concessões visíveis para gerenciamento.
func (s *PermissionService) ListForUser(ctx context.Context, userID string) ([]domain.UserAppPermission, error) {
	return s.store.ListAppPermissionsForUser(ctx, userID)
}
