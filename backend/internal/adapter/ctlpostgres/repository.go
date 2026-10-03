// Package ctlpostgres implementa o control plane (usuários e chaves de API)
// sobre PostgreSQL — dados transacionais e mutáveis da própria ferramenta.
// Os traces NÃO passam por aqui: vivem no ClickHouse, imutáveis.
package ctlpostgres

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Repository implementa domain.UserStore e domain.APIKeyStore.
type Repository struct {
	pool *pgxpool.Pool
}

// Connect abre o pool aguardando o banco subir (compose) e aplica migrações.
func Connect(ctx context.Context, databaseURL string, logger *slog.Logger) (*Repository, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parseando NDOVU_POSTGRES_URL: %w", err)
	}
	cfg.MaxConns = 5
	cfg.MaxConnLifetime = 30 * time.Minute

	var pool *pgxpool.Pool
	deadline := time.Now().Add(60 * time.Second)
	for {
		pool, err = pgxpool.NewWithConfig(ctx, cfg)
		if err == nil {
			if pingErr := pool.Ping(ctx); pingErr == nil {
				break
			} else {
				err = pingErr
				pool.Close()
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("postgres indisponível após 60s: %w", err)
		}
		logger.Warn("aguardando postgres...", "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	r := &Repository{pool: pool}
	if err := r.migrate(ctx, logger); err != nil {
		pool.Close()
		return nil, err
	}
	return r, nil
}

// Close encerra o pool.
func (r *Repository) Close() { r.pool.Close() }

// migrate aplica as migrações embedadas com advisory lock (seguro com réplicas).
func (r *Repository) migrate(ctx context.Context, logger *slog.Logger) error {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("adquirindo conexão: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(834551)`); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}
	defer func() { _, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock(834551)`) }()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("criando schema_migrations: %w", err)
	}

	entries, err := fs.Glob(migrationsFS, "migrations/*.up.sql")
	if err != nil {
		return fmt.Errorf("listando migrações: %w", err)
	}
	sort.Strings(entries)

	for _, path := range entries {
		version := strings.TrimSuffix(strings.TrimPrefix(path, "migrations/"), ".up.sql")
		var exists bool
		if err := conn.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).
			Scan(&exists); err != nil {
			return fmt.Errorf("verificando versão %s: %w", version, err)
		}
		if exists {
			continue
		}
		sql, err := migrationsFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("lendo %s: %w", path, err)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("abrindo transação: %w", err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("aplicando %s: %w", version, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("registrando %s: %w", version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit %s: %w", version, err)
		}
		logger.Info("migração de control plane aplicada", "version", version)
	}
	return nil
}

// ---------------------------------------------------------------------------
// UserStore
// ---------------------------------------------------------------------------

// userSelect resolve o nome da empresa junto com o usuário para o backoffice
// exibir sem outra query. company_id é NOT NULL após a migração 000004;
// is_super chegou na 000010 (Fase 3 sprint E.3, multi-tenancy).
const userSelect = `u.id, u.email, u.name, u.role, u.active, u.is_super,
	u.company_id, c.name AS company,
	u.created_at, u.updated_at`

// CreateUser insere um usuário; e-mail duplicado vira ErrConflict.
func (r *Repository) CreateUser(ctx context.Context, u domain.User, passwordHash string) (domain.User, error) {
	if u.CompanyID == "" {
		return domain.User{}, domain.NewValidationError("companyId é obrigatório")
	}
	err := r.pool.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO users (email, name, password_hash, role, active, company_id)
			VALUES ($1, $2, $3, $4, $5, $6::uuid)
			RETURNING id, email, name, role, active, is_super, company_id, created_at, updated_at
		)
		SELECT `+userSelect+` FROM inserted u JOIN companies c ON c.id = u.company_id`,
		strings.ToLower(u.Email), u.Name, passwordHash, string(u.Role), u.Active, u.CompanyID).
		Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Active, &u.IsSuper, &u.CompanyID, &u.Company, &u.CreatedAt, &u.UpdatedAt)
	if isUniqueViolation(err) {
		return domain.User{}, fmt.Errorf("e-mail %s: %w", u.Email, domain.ErrConflict)
	}
	if isForeignKeyViolation(err) {
		return domain.User{}, fmt.Errorf("empresa: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("criando usuário: %w", err)
	}
	return u, nil
}

// GetUserByEmail retorna o usuário e o hash da senha para verificação de login.
func (r *Repository) GetUserByEmail(ctx context.Context, email string) (domain.User, string, error) {
	var u domain.User
	var hash string
	err := r.pool.QueryRow(ctx,
		`SELECT `+userSelect+`, u.password_hash FROM users u JOIN companies c ON c.id = u.company_id WHERE u.email = $1`,
		strings.ToLower(email)).
		Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Active, &u.IsSuper, &u.CompanyID, &u.Company, &u.CreatedAt, &u.UpdatedAt, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, "", domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, "", fmt.Errorf("consultando usuário: %w", err)
	}
	return u, hash, nil
}

// GetUserByID retorna um usuário por id.
func (r *Repository) GetUserByID(ctx context.Context, id string) (domain.User, error) {
	var u domain.User
	err := r.pool.QueryRow(ctx,
		`SELECT `+userSelect+` FROM users u JOIN companies c ON c.id = u.company_id WHERE u.id = $1`, id).
		Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Active, &u.IsSuper, &u.CompanyID, &u.Company, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("consultando usuário: %w", err)
	}
	return u, nil
}

// ListUsers lista todos os usuários com o nome da empresa resolvido.
// Cross-company: use só para super-admin. Admin normal deve chamar
// ListUsersByCompany para não vazar dados de outras companies.
func (r *Repository) ListUsers(ctx context.Context) ([]domain.User, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+userSelect+` FROM users u JOIN companies c ON c.id = u.company_id ORDER BY u.created_at`)
	if err != nil {
		return nil, fmt.Errorf("listando usuários: %w", err)
	}
	defer rows.Close()
	users := []domain.User{}
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Active, &u.IsSuper,
			&u.CompanyID, &u.Company, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// ListUsersByCompany lista apenas usuários pertencentes a uma company —
// usado pelos handlers admin para isolar cross-tenant.
func (r *Repository) ListUsersByCompany(ctx context.Context, companyID string) ([]domain.User, error) {
	if companyID == "" {
		return []domain.User{}, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+userSelect+` FROM users u JOIN companies c ON c.id = u.company_id
		 WHERE u.company_id = $1::uuid ORDER BY u.created_at`, companyID)
	if err != nil {
		return nil, fmt.Errorf("listando usuários por company: %w", err)
	}
	defer rows.Close()
	users := []domain.User{}
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Active, &u.IsSuper,
			&u.CompanyID, &u.Company, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// UpdateUser altera role/active/name/companyId (campos nil ficam como estão).
func (r *Repository) UpdateUser(ctx context.Context, id string, role *domain.Role, active *bool, name *string, companyID *string) (domain.User, error) {
	var u domain.User
	err := r.pool.QueryRow(ctx, `
		WITH updated AS (
			UPDATE users SET
				role       = COALESCE($2, role),
				active     = COALESCE($3, active),
				name       = COALESCE($4, name),
				company_id = COALESCE(NULLIF($5, '')::uuid, company_id),
				updated_at = now()
			WHERE id = $1
			RETURNING id, email, name, role, active, is_super, company_id, created_at, updated_at
		)
		SELECT `+userSelect+` FROM updated u JOIN companies c ON c.id = u.company_id`,
		id, roleArg(role), active, name, ptrString(companyID)).
		Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Active, &u.IsSuper, &u.CompanyID, &u.Company, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if isForeignKeyViolation(err) {
		return domain.User{}, fmt.Errorf("empresa: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("atualizando usuário: %w", err)
	}
	return u, nil
}

// SetPassword troca o hash de senha de um usuário.
func (r *Repository) SetPassword(ctx context.Context, id string, passwordHash string) error {
	ct, err := r.pool.Exec(ctx,
		`UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, id, passwordHash)
	if err != nil {
		return fmt.Errorf("trocando senha: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// SetSuperAdmin grants super-admin to the configured bootstrap account.
func (r *Repository) SetSuperAdmin(ctx context.Context, id string) error {
	ct, err := r.pool.Exec(ctx,
		`UPDATE users SET is_super = true, updated_at = now() WHERE id = $1 AND role = 'admin'`, id)
	if err != nil {
		return fmt.Errorf("promovendo usuário a super-admin: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// CountActiveAdmins conta admins ativos (protege contra remover o último).
func (r *Repository) CountActiveAdmins(ctx context.Context) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE role = 'admin' AND active`).Scan(&n)
	return n, err
}

// ---------------------------------------------------------------------------
// APIKeyStore
// ---------------------------------------------------------------------------

const keyColumns = `id, COALESCE(app_id::text, '') AS app_id, app, label, key_prefix, active, created_at, revoked_at`

// CreateAPIKey revoga a chave ativa anterior do app e registra a nova na mesma
// transação, preservando o histórico e evitando duas chaves ativas concorrentes.
func (r *Repository) CreateAPIKey(ctx context.Context, k domain.APIKey, keyHash string, createdBy string) (domain.APIKey, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.APIKey{}, fmt.Errorf("iniciando transação de criação de chave: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if k.AppID != "" {
		var lockedAppID string
		if err := tx.QueryRow(ctx, `SELECT id FROM apps WHERE id = $1 FOR UPDATE`, k.AppID).Scan(&lockedAppID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.APIKey{}, domain.ErrNotFound
			}
			return domain.APIKey{}, fmt.Errorf("bloqueando app para rotação de chave: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE api_keys SET active = false, revoked_at = now() WHERE app_id = $1 AND active`,
			k.AppID); err != nil {
			return domain.APIKey{}, fmt.Errorf("revogando chave anterior do app: %w", err)
		}
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO api_keys (app_id, app, label, key_hash, key_prefix, key_ciphertext, created_by)
		VALUES (NULLIF($1, '')::uuid, $2, $3, $4, $5, $6, NULLIF($7, '')::uuid)
		RETURNING `+keyColumns+`, key_ciphertext`,
		k.AppID, k.App, k.Label, keyHash, k.Prefix, k.EncryptedKey, createdBy).
		Scan(&k.ID, &k.AppID, &k.App, &k.Label, &k.Prefix, &k.Active, &k.CreatedAt, &k.RevokedAt, &k.EncryptedKey)
	if isUniqueViolation(err) {
		return domain.APIKey{}, domain.ErrConflict
	}
	if err != nil {
		return domain.APIKey{}, fmt.Errorf("criando chave: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.APIKey{}, fmt.Errorf("confirmando criação de chave: %w", err)
	}
	return k, nil
}

// ListAPIKeys lista todas as chaves (sem hash). Cross-company: use só para
// super-admin. Admin normal deve chamar ListAPIKeysByCompany.
func (r *Repository) ListAPIKeys(ctx context.Context) ([]domain.APIKey, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+keyColumns+`, key_ciphertext FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("listando chaves: %w", err)
	}
	defer rows.Close()
	keys := []domain.APIKey{}
	for rows.Next() {
		var k domain.APIKey
		if err := rows.Scan(&k.ID, &k.AppID, &k.App, &k.Label, &k.Prefix, &k.Active, &k.CreatedAt, &k.RevokedAt, &k.EncryptedKey); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// ListAPIKeysByCompany filtra chaves cuja app pertence à company informada.
// Chaves antigas sem app_id (legado) não aparecem — o cadastro atual sempre
// vincula app+key, então o filtro é seguro.
func (r *Repository) ListAPIKeysByCompany(ctx context.Context, companyID string) ([]domain.APIKey, error) {
	if companyID == "" {
		return []domain.APIKey{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT k.id, COALESCE(k.app_id::text, '') AS app_id, k.app, k.label, k.key_prefix,
		       k.active, k.created_at, k.revoked_at, k.key_ciphertext
		FROM api_keys k
		JOIN apps a ON a.id = k.app_id
		WHERE a.company_id = $1::uuid
		ORDER BY k.created_at DESC`, companyID)
	if err != nil {
		return nil, fmt.Errorf("listando chaves por company: %w", err)
	}
	defer rows.Close()
	keys := []domain.APIKey{}
	for rows.Next() {
		var k domain.APIKey
		if err := rows.Scan(&k.ID, &k.AppID, &k.App, &k.Label, &k.Prefix, &k.Active, &k.CreatedAt, &k.RevokedAt, &k.EncryptedKey); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// RevokeAPIKey desativa uma chave (não deleta: auditoria).
func (r *Repository) RevokeAPIKey(ctx context.Context, id string) error {
	ct, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET active = false, revoked_at = now() WHERE id = $1 AND active`, id)
	if err != nil {
		return fmt.Errorf("revogando chave: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// FindActiveKeyByHash resolve o hash de uma chave apresentada na ingestão.
func (r *Repository) FindActiveKeyByHash(ctx context.Context, keyHash string) (domain.APIKey, error) {
	var k domain.APIKey
	err := r.pool.QueryRow(ctx,
		`SELECT `+keyColumns+` FROM api_keys WHERE key_hash = $1 AND active`, keyHash).
		Scan(&k.ID, &k.AppID, &k.App, &k.Label, &k.Prefix, &k.Active, &k.CreatedAt, &k.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.APIKey{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.APIKey{}, fmt.Errorf("consultando chave: %w", err)
	}
	return k, nil
}

// GetAPIKeyByID resolve uma chave por id (sem hash). Usado pelo handler
// de revogação para checar ownership da company antes de mutar.
func (r *Repository) GetAPIKeyByID(ctx context.Context, id string) (domain.APIKey, error) {
	var k domain.APIKey
	err := r.pool.QueryRow(ctx,
		`SELECT `+keyColumns+` FROM api_keys WHERE id = $1`, id).
		Scan(&k.ID, &k.AppID, &k.App, &k.Label, &k.Prefix, &k.Active, &k.CreatedAt, &k.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.APIKey{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.APIKey{}, fmt.Errorf("consultando chave: %w", err)
	}
	return k, nil
}

// ---------------------------------------------------------------------------
// AppStore
// ---------------------------------------------------------------------------

// appSelect resolve o nome da empresa via LEFT JOIN (apps.company_id é opcional).
// Fallback para o campo texto legado apps.company quando não houver FK.
const appSelect = `a.id, a.name, a.technology,
	COALESCE(a.company_id::text, '') AS company_id,
	COALESCE(c.name, a.company) AS company,
	a.responsible, a.created_at, a.updated_at`

// CreateApp insere um app emissor; nome duplicado vira ErrConflict.
func (r *Repository) CreateApp(ctx context.Context, a domain.App) (domain.App, error) {
	err := r.pool.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO apps (name, technology, company, responsible, company_id)
			VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid)
			RETURNING id, name, technology, company, responsible, company_id, created_at, updated_at
		)
		SELECT `+appSelect+` FROM inserted a LEFT JOIN companies c ON c.id = a.company_id`,
		a.Name, a.Technology, a.Company, a.Responsible, a.CompanyID).
		Scan(&a.ID, &a.Name, &a.Technology, &a.CompanyID, &a.Company, &a.Responsible, &a.CreatedAt, &a.UpdatedAt)
	if isUniqueViolation(err) {
		return domain.App{}, fmt.Errorf("app %s: %w", a.Name, domain.ErrConflict)
	}
	if isForeignKeyViolation(err) {
		return domain.App{}, fmt.Errorf("empresa: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.App{}, fmt.Errorf("criando app: %w", err)
	}
	return a, nil
}

// GetApp retorna um app por id.
func (r *Repository) GetApp(ctx context.Context, id string) (domain.App, error) {
	var a domain.App
	err := r.pool.QueryRow(ctx,
		`SELECT `+appSelect+` FROM apps a LEFT JOIN companies c ON c.id = a.company_id WHERE a.id = $1`, id).
		Scan(&a.ID, &a.Name, &a.Technology, &a.CompanyID, &a.Company, &a.Responsible, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.App{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.App{}, fmt.Errorf("consultando app: %w", err)
	}
	return a, nil
}

// GetAppByName resolve um app pelo campo `name` (usado no ownership check
// de chaves de API, cuja request só carrega o nome do app).
func (r *Repository) GetAppByName(ctx context.Context, name string) (domain.App, error) {
	var a domain.App
	err := r.pool.QueryRow(ctx,
		`SELECT `+appSelect+` FROM apps a LEFT JOIN companies c ON c.id = a.company_id WHERE a.name = $1`, name).
		Scan(&a.ID, &a.Name, &a.Technology, &a.CompanyID, &a.Company, &a.Responsible, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.App{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.App{}, fmt.Errorf("consultando app por nome: %w", err)
	}
	return a, nil
}

// ListApps lista todos os apps cadastrados. Cross-company: use só para
// super-admin. Admin normal deve chamar ListAppsByCompany.
func (r *Repository) ListApps(ctx context.Context) ([]domain.App, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+appSelect+` FROM apps a LEFT JOIN companies c ON c.id = a.company_id ORDER BY a.name`)
	if err != nil {
		return nil, fmt.Errorf("listando apps: %w", err)
	}
	defer rows.Close()
	apps := []domain.App{}
	for rows.Next() {
		var a domain.App
		if err := rows.Scan(&a.ID, &a.Name, &a.Technology, &a.CompanyID, &a.Company,
			&a.Responsible, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, rows.Err()
}

// ListAppsByCompany filtra apps por company (para isolamento no admin).
func (r *Repository) ListAppsByCompany(ctx context.Context, companyID string) ([]domain.App, error) {
	if companyID == "" {
		return []domain.App{}, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+appSelect+` FROM apps a LEFT JOIN companies c ON c.id = a.company_id
		 WHERE a.company_id = $1::uuid ORDER BY a.name`, companyID)
	if err != nil {
		return nil, fmt.Errorf("listando apps por company: %w", err)
	}
	defer rows.Close()
	apps := []domain.App{}
	for rows.Next() {
		var a domain.App
		if err := rows.Scan(&a.ID, &a.Name, &a.Technology, &a.CompanyID, &a.Company,
			&a.Responsible, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, rows.Err()
}

// UpdateApp altera os metadados de um app (campos vazios sobrescrevem).
func (r *Repository) UpdateApp(ctx context.Context, id string, a domain.App) (domain.App, error) {
	var out domain.App
	err := r.pool.QueryRow(ctx, `
		WITH updated AS (
			UPDATE apps SET
				name        = $2,
				technology  = $3,
				company     = $4,
				responsible = $5,
				company_id  = NULLIF($6, '')::uuid,
				updated_at  = now()
			WHERE id = $1
			RETURNING id, name, technology, company, responsible, company_id, created_at, updated_at
		)
		SELECT `+appSelect+` FROM updated a LEFT JOIN companies c ON c.id = a.company_id`,
		id, a.Name, a.Technology, a.Company, a.Responsible, a.CompanyID).
		Scan(&out.ID, &out.Name, &out.Technology, &out.CompanyID, &out.Company, &out.Responsible,
			&out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.App{}, domain.ErrNotFound
	}
	if isUniqueViolation(err) {
		return domain.App{}, fmt.Errorf("app %s: %w", a.Name, domain.ErrConflict)
	}
	if isForeignKeyViolation(err) {
		return domain.App{}, fmt.Errorf("empresa: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.App{}, fmt.Errorf("atualizando app: %w", err)
	}
	return out, nil
}

// ListAppNamesByCompany devolve os nomes dos apps que pertencem a uma
// company específica — usado para restringir o escopo de queries no
// ClickHouse (que segrega por `app`, não por company_id).
func (r *Repository) ListAppNamesByCompany(ctx context.Context, companyID string) ([]string, error) {
	if companyID == "" {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT name FROM apps WHERE company_id = $1::uuid ORDER BY name`, companyID)
	if err != nil {
		return nil, fmt.Errorf("listando apps por company: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// DeleteApp remove um app. As chaves associadas são revogadas antes pelo
// usecase; aqui desvinculamos a FK (app_id → NULL) preservando o registro de
// auditoria das chaves, e então removemos o app.
func (r *Repository) DeleteApp(ctx context.Context, id string) error {
	if _, err := r.pool.Exec(ctx, `UPDATE api_keys SET app_id = NULL WHERE app_id = $1`, id); err != nil {
		return fmt.Errorf("desvinculando chaves do app: %w", err)
	}
	ct, err := r.pool.Exec(ctx, `DELETE FROM apps WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deletando app: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// CompanyStore
// ---------------------------------------------------------------------------

const companyColumns = `id, name, document, active, created_at, updated_at`

// CreateCompany insere uma empresa; nome duplicado vira ErrConflict.
func (r *Repository) CreateCompany(ctx context.Context, c domain.Company) (domain.Company, error) {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO companies (name, document, active)
		VALUES ($1, $2, $3)
		RETURNING `+companyColumns,
		c.Name, c.Document, c.Active).
		Scan(&c.ID, &c.Name, &c.Document, &c.Active, &c.CreatedAt, &c.UpdatedAt)
	if isUniqueViolation(err) {
		return domain.Company{}, fmt.Errorf("empresa %s: %w", c.Name, domain.ErrConflict)
	}
	if err != nil {
		return domain.Company{}, fmt.Errorf("criando empresa: %w", err)
	}
	return c, nil
}

// GetCompany retorna uma empresa por id.
func (r *Repository) GetCompany(ctx context.Context, id string) (domain.Company, error) {
	var c domain.Company
	err := r.pool.QueryRow(ctx, `SELECT `+companyColumns+` FROM companies WHERE id = $1`, id).
		Scan(&c.ID, &c.Name, &c.Document, &c.Active, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Company{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Company{}, fmt.Errorf("consultando empresa: %w", err)
	}
	return c, nil
}

// ListCompanies lista todas as empresas.
func (r *Repository) ListCompanies(ctx context.Context) ([]domain.Company, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+companyColumns+` FROM companies ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("listando empresas: %w", err)
	}
	defer rows.Close()
	out := []domain.Company{}
	for rows.Next() {
		var c domain.Company
		if err := rows.Scan(&c.ID, &c.Name, &c.Document, &c.Active, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateCompany altera nome/documento/ativação.
func (r *Repository) UpdateCompany(ctx context.Context, id string, c domain.Company) (domain.Company, error) {
	var out domain.Company
	err := r.pool.QueryRow(ctx, `
		UPDATE companies SET
			name = $2, document = $3, active = $4, updated_at = now()
		WHERE id = $1
		RETURNING `+companyColumns,
		id, c.Name, c.Document, c.Active).
		Scan(&out.ID, &out.Name, &out.Document, &out.Active, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Company{}, domain.ErrNotFound
	}
	if isUniqueViolation(err) {
		return domain.Company{}, fmt.Errorf("empresa %s: %w", c.Name, domain.ErrConflict)
	}
	if err != nil {
		return domain.Company{}, fmt.Errorf("atualizando empresa: %w", err)
	}
	return out, nil
}

// DeleteCompany remove uma empresa — bloqueia se ainda houver usuários vinculados.
func (r *Repository) DeleteCompany(ctx context.Context, id string) error {
	var users int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE company_id = $1`, id).Scan(&users); err != nil {
		return fmt.Errorf("verificando usuários vinculados: %w", err)
	}
	if users > 0 {
		return domain.NewValidationError(
			fmt.Sprintf("empresa possui %d usuário(s) vinculado(s) — remova/transfira antes", users),
		)
	}
	// Apps podem existir com FK: setamos para NULL preservando o texto legado.
	if _, err := r.pool.Exec(ctx, `UPDATE apps SET company_id = NULL WHERE company_id = $1`, id); err != nil {
		return fmt.Errorf("desvinculando apps: %w", err)
	}
	ct, err := r.pool.Exec(ctx, `DELETE FROM companies WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deletando empresa: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// IssueStore — estado mutável de issues (agrupamento vem do ClickHouse)
// ---------------------------------------------------------------------------

// GetIssueStates retorna o estado (status/assignee + dados do usuário) para
// um conjunto de fingerprints. Só devolve os que tem estado persistido; os
// demais são implicitamente "open".
func (r *Repository) GetIssueStates(ctx context.Context, fingerprints []string) (map[string]domain.IssueState, error) {
	states := map[string]domain.IssueState{}
	if len(fingerprints) == 0 {
		return states, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT s.fingerprint, s.status, s.assignee,
		       COALESCE(s.assignee_user_id::text, '') AS assignee_user_id,
		       COALESCE(u.name, '') AS assignee_name,
		       COALESCE(u.email, '') AS assignee_email
		FROM issue_states s
		LEFT JOIN users u ON u.id = s.assignee_user_id
		WHERE s.fingerprint = ANY($1)`,
		fingerprints)
	if err != nil {
		return nil, fmt.Errorf("consultando issue_states: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var fp string
		var st domain.IssueState
		if err := rows.Scan(&fp, &st.Status, &st.Assignee,
			&st.AssigneeUserID, &st.AssigneeName, &st.AssigneeEmail); err != nil {
			return nil, err
		}
		states[fp] = st
	}
	return states, rows.Err()
}

// UpsertIssueStatus grava/atualiza o estado. assigneeUserId vazio = desatribui;
// não-vazio = grava o FK (também sobrescreve o texto legado com o email do user).
func (r *Repository) UpsertIssueStatus(ctx context.Context, in domain.IssueStatusInput) error {
	if !in.Status.Valid() {
		return domain.NewValidationError("status inválido")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO issue_states (fingerprint, app, status, assignee, assignee_user_id, updated_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid, now())
		ON CONFLICT (fingerprint) DO UPDATE
		  SET status = EXCLUDED.status,
		      assignee = EXCLUDED.assignee,
		      assignee_user_id = EXCLUDED.assignee_user_id,
		      updated_at = now()`,
		in.Fingerprint, in.App, string(in.Status), in.Assignee, in.AssigneeUserID)
	if isForeignKeyViolation(err) {
		return fmt.Errorf("usuário assignee: %w", domain.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("upsert issue_state: %w", err)
	}
	return nil
}

// ListIssueComments retorna os comentários mais recentes primeiro, com o
// nome+email do autor resolvidos via join.
func (r *Repository) ListIssueComments(ctx context.Context, fingerprint string) ([]domain.IssueComment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.fingerprint,
		       COALESCE(c.author_id::text, ''),
		       COALESCE(u.name, ''), COALESCE(u.email, ''),
		       c.body, c.created_at
		FROM issue_comments c
		LEFT JOIN users u ON u.id = c.author_id
		WHERE c.fingerprint = $1
		ORDER BY c.created_at DESC`, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("listando comentários: %w", err)
	}
	defer rows.Close()
	out := []domain.IssueComment{}
	for rows.Next() {
		var c domain.IssueComment
		if err := rows.Scan(&c.ID, &c.Fingerprint, &c.AuthorID,
			&c.AuthorName, &c.AuthorEmail, &c.Body, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateIssueComment insere um comentário. Devolve com autor resolvido.
func (r *Repository) CreateIssueComment(ctx context.Context, fingerprint, authorID, body string) (domain.IssueComment, error) {
	var c domain.IssueComment
	err := r.pool.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO issue_comments (fingerprint, author_id, body)
			VALUES ($1, NULLIF($2, '')::uuid, $3)
			RETURNING id, fingerprint, author_id, body, created_at
		)
		SELECT i.id, i.fingerprint,
		       COALESCE(i.author_id::text, ''),
		       COALESCE(u.name, ''), COALESCE(u.email, ''),
		       i.body, i.created_at
		FROM inserted i
		LEFT JOIN users u ON u.id = i.author_id`,
		fingerprint, authorID, body).
		Scan(&c.ID, &c.Fingerprint, &c.AuthorID, &c.AuthorName, &c.AuthorEmail, &c.Body, &c.CreatedAt)
	if err != nil {
		return domain.IssueComment{}, fmt.Errorf("criando comentário: %w", err)
	}
	return c, nil
}

// DeleteIssueComment só permite o próprio autor apagar. Silenciosamente 404
// se o id não existe ou se o requester não é o autor — evita expor existência.
func (r *Repository) DeleteIssueComment(ctx context.Context, id, requesterID string) error {
	ct, err := r.pool.Exec(ctx,
		`DELETE FROM issue_comments WHERE id = $1 AND author_id = NULLIF($2, '')::uuid`,
		id, requesterID)
	if err != nil {
		return fmt.Errorf("removendo comentário: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ListFingerprintsByAssignee retorna os fingerprints atribuídos a um usuário —
// usado para filtrar "atribuídas a mim" na listagem de issues.
func (r *Repository) ListFingerprintsByAssignee(ctx context.Context, userID string) ([]string, error) {
	if userID == "" {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT fingerprint FROM issue_states WHERE assignee_user_id = $1::uuid`, userID)
	if err != nil {
		return nil, fmt.Errorf("listando fingerprints por assignee: %w", err)
	}
	defer rows.Close()
	fps := []string{}
	for rows.Next() {
		var fp string
		if err := rows.Scan(&fp); err != nil {
			return nil, err
		}
		fps = append(fps, fp)
	}
	return fps, rows.Err()
}

// ---------------------------------------------------------------------------
// AlertRuleStore — regras de alerta e histórico de disparos
// ---------------------------------------------------------------------------

const alertColumns = `id, name, app, error_code, threshold, window_seconds, channel, target_url, silence_seconds, active, created_at, updated_at`

// CreateAlertRule insere uma regra de alerta.
func (r *Repository) CreateAlertRule(ctx context.Context, rule domain.AlertRule) (domain.AlertRule, error) {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO alert_rules (name, app, error_code, threshold, window_seconds, channel, target_url, silence_seconds, active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+alertColumns,
		rule.Name, rule.App, rule.ErrorCode, rule.Threshold, rule.WindowSeconds,
		rule.Channel, rule.TargetURL, rule.SilenceSeconds, rule.Active).
		Scan(&rule.ID, &rule.Name, &rule.App, &rule.ErrorCode, &rule.Threshold,
			&rule.WindowSeconds, &rule.Channel, &rule.TargetURL, &rule.SilenceSeconds,
			&rule.Active, &rule.CreatedAt, &rule.UpdatedAt)
	if err != nil {
		return domain.AlertRule{}, fmt.Errorf("criando alerta: %w", err)
	}
	return rule, nil
}

// GetAlertRule resolve uma regra por id (usado no ownership check).
func (r *Repository) GetAlertRule(ctx context.Context, id string) (domain.AlertRule, error) {
	var a domain.AlertRule
	err := r.pool.QueryRow(ctx,
		`SELECT `+alertColumns+` FROM alert_rules WHERE id = $1`, id).
		Scan(&a.ID, &a.Name, &a.App, &a.ErrorCode, &a.Threshold,
			&a.WindowSeconds, &a.Channel, &a.TargetURL, &a.SilenceSeconds,
			&a.Active, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AlertRule{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.AlertRule{}, fmt.Errorf("consultando alerta: %w", err)
	}
	return a, nil
}

// ListAlertRules lista todas as regras (ativas e inativas).
func (r *Repository) ListAlertRules(ctx context.Context) ([]domain.AlertRule, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+alertColumns+` FROM alert_rules ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("listando alertas: %w", err)
	}
	defer rows.Close()
	rules := []domain.AlertRule{}
	for rows.Next() {
		var a domain.AlertRule
		if err := rows.Scan(&a.ID, &a.Name, &a.App, &a.ErrorCode, &a.Threshold,
			&a.WindowSeconds, &a.Channel, &a.TargetURL, &a.SilenceSeconds,
			&a.Active, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		rules = append(rules, a)
	}
	return rules, rows.Err()
}

// DeleteAlertRule remove uma regra (e cascata nos deliveries).
func (r *Repository) DeleteAlertRule(ctx context.Context, id string) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM alert_rules WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("removendo alerta: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// LastDeliveryAt devolve quando a regra disparou pela última vez com sucesso —
// usado para respeitar a janela de silêncio (dedup de alertas).
func (r *Repository) LastDeliveryAt(ctx context.Context, ruleID string) (time.Time, error) {
	var t time.Time
	err := r.pool.QueryRow(ctx,
		`SELECT delivered_at FROM alert_deliveries WHERE rule_id = $1 AND ok ORDER BY delivered_at DESC LIMIT 1`,
		ruleID).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("último delivery: %w", err)
	}
	return t, nil
}

// RecordDelivery grava um disparo de alerta (para auditoria + dedup).
func (r *Repository) RecordDelivery(ctx context.Context, ruleID string, count int, ok bool, detail string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO alert_deliveries (rule_id, count_seen, ok, detail) VALUES ($1, $2, $3, $4)`,
		ruleID, count, ok, detail)
	if err != nil {
		return fmt.Errorf("gravando delivery: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// FeedbackStore (Fase 4 sprint J) — user feedback widget
// ---------------------------------------------------------------------------

const feedbackCols = `id, app, session_id, event_id, user_id, type, message,
	email, url, viewport_w, viewport_h, status,
	COALESCE(resolved_by::text, ''), resolved_at, created_at`

// CreateFeedback grava um feedback vindo do widget.
func (r *Repository) CreateFeedback(ctx context.Context, f domain.UserFeedback) (domain.UserFeedback, error) {
	if f.Type == "" {
		f.Type = domain.FeedbackBug
	}
	if f.Status == "" {
		f.Status = domain.FeedbackNew
	}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO user_feedbacks
			(app, session_id, event_id, user_id, type, message, email, url,
			 viewport_w, viewport_h, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING `+feedbackCols,
		f.App, f.SessionID, f.EventID, f.UserID, string(f.Type), f.Message,
		f.Email, f.URL, f.ViewportW, f.ViewportH, string(f.Status)).
		Scan(&f.ID, &f.App, &f.SessionID, &f.EventID, &f.UserID, &f.Type, &f.Message,
			&f.Email, &f.URL, &f.ViewportW, &f.ViewportH, &f.Status,
			&f.ResolvedBy, &f.ResolvedAt, &f.CreatedAt)
	if err != nil {
		return domain.UserFeedback{}, fmt.Errorf("criando feedback: %w", err)
	}
	return f, nil
}

// UpdateFeedbackStatus altera status + resolvedBy/resolvedAt.
func (r *Repository) UpdateFeedbackStatus(ctx context.Context, id string, status domain.FeedbackStatus, resolvedBy string) (domain.UserFeedback, error) {
	var f domain.UserFeedback
	// resolved_at só é setado quando muda para resolved/dismissed.
	err := r.pool.QueryRow(ctx, `
		UPDATE user_feedbacks SET
			status = $2,
			resolved_by = CASE WHEN $2 IN ('resolved','dismissed') THEN NULLIF($3, '')::uuid ELSE NULL END,
			resolved_at = CASE WHEN $2 IN ('resolved','dismissed') THEN now() ELSE NULL END
		WHERE id = $1
		RETURNING `+feedbackCols,
		id, string(status), resolvedBy).
		Scan(&f.ID, &f.App, &f.SessionID, &f.EventID, &f.UserID, &f.Type, &f.Message,
			&f.Email, &f.URL, &f.ViewportW, &f.ViewportH, &f.Status,
			&f.ResolvedBy, &f.ResolvedAt, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.UserFeedback{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.UserFeedback{}, fmt.Errorf("atualizando feedback: %w", err)
	}
	return f, nil
}

// GetFeedback resolve um feedback por id (usado no ownership check).
func (r *Repository) GetFeedback(ctx context.Context, id string) (domain.UserFeedback, error) {
	var fb domain.UserFeedback
	err := r.pool.QueryRow(ctx,
		`SELECT `+feedbackCols+` FROM user_feedbacks WHERE id = $1`, id).
		Scan(&fb.ID, &fb.App, &fb.SessionID, &fb.EventID, &fb.UserID,
			&fb.Type, &fb.Message, &fb.Email, &fb.URL, &fb.ViewportW, &fb.ViewportH,
			&fb.Status, &fb.ResolvedBy, &fb.ResolvedAt, &fb.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.UserFeedback{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.UserFeedback{}, fmt.Errorf("consultando feedback: %w", err)
	}
	return fb, nil
}

// ListFeedbacks devolve página + total (para paginação).
func (r *Repository) ListFeedbacks(ctx context.Context, f domain.FeedbackFilter) ([]domain.UserFeedback, int, error) {
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}

	where := []string{"1=1"}
	args := []any{}
	add := func(clause string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if f.App != "" {
		add("app = $%d", f.App)
	}
	if f.Status != "" {
		add("status = $%d", f.Status)
	}
	if f.SessionID != "" {
		add("session_id = $%d", f.SessionID)
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := r.pool.QueryRow(ctx,
		"SELECT count(*) FROM user_feedbacks WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, f.Offset)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT `+feedbackCols+` FROM user_feedbacks WHERE %s
		ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		whereSQL, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("listando feedbacks: %w", err)
	}
	defer rows.Close()
	out := []domain.UserFeedback{}
	for rows.Next() {
		var fb domain.UserFeedback
		if err := rows.Scan(&fb.ID, &fb.App, &fb.SessionID, &fb.EventID, &fb.UserID,
			&fb.Type, &fb.Message, &fb.Email, &fb.URL, &fb.ViewportW, &fb.ViewportH,
			&fb.Status, &fb.ResolvedBy, &fb.ResolvedAt, &fb.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, fb)
	}
	return out, total, rows.Err()
}

// DeleteFeedback remove por id (útil para spam manifesto).
func (r *Repository) DeleteFeedback(ctx context.Context, id string) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM user_feedbacks WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("removendo feedback: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// AnomalyStore (Fase 4 sprint I) — detecção de anomalia
// ---------------------------------------------------------------------------

const anomalyCols = `id, name, app, metric, window_minutes, baseline_weeks,
	sensitivity, direction, silence_seconds, channel, target_url, active,
	created_at, updated_at`

func (r *Repository) CreateAnomalyRule(ctx context.Context, rule domain.AnomalyRule) (domain.AnomalyRule, error) {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO anomaly_rules (name, app, metric, window_minutes, baseline_weeks,
			sensitivity, direction, silence_seconds, channel, target_url, active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING `+anomalyCols,
		rule.Name, rule.App, string(rule.Metric), rule.WindowMinutes, rule.BaselineWeeks,
		rule.Sensitivity, string(rule.Direction), rule.SilenceSeconds,
		string(rule.Channel), rule.TargetURL, rule.Active).
		Scan(&rule.ID, &rule.Name, &rule.App, &rule.Metric, &rule.WindowMinutes,
			&rule.BaselineWeeks, &rule.Sensitivity, &rule.Direction, &rule.SilenceSeconds,
			&rule.Channel, &rule.TargetURL, &rule.Active, &rule.CreatedAt, &rule.UpdatedAt)
	if err != nil {
		return domain.AnomalyRule{}, fmt.Errorf("criando anomaly rule: %w", err)
	}
	return rule, nil
}

// GetAnomalyRule resolve uma regra por id (usado no ownership check).
func (r *Repository) GetAnomalyRule(ctx context.Context, id string) (domain.AnomalyRule, error) {
	var a domain.AnomalyRule
	err := r.pool.QueryRow(ctx,
		`SELECT `+anomalyCols+` FROM anomaly_rules WHERE id = $1`, id).
		Scan(&a.ID, &a.Name, &a.App, &a.Metric, &a.WindowMinutes,
			&a.BaselineWeeks, &a.Sensitivity, &a.Direction, &a.SilenceSeconds,
			&a.Channel, &a.TargetURL, &a.Active, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AnomalyRule{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.AnomalyRule{}, fmt.Errorf("consultando anomaly rule: %w", err)
	}
	return a, nil
}

func (r *Repository) UpdateAnomalyRule(ctx context.Context, id string, rule domain.AnomalyRule) (domain.AnomalyRule, error) {
	err := r.pool.QueryRow(ctx, `
		UPDATE anomaly_rules SET
			name = $2, app = $3, metric = $4, window_minutes = $5, baseline_weeks = $6,
			sensitivity = $7, direction = $8, silence_seconds = $9, channel = $10,
			target_url = $11, active = $12, updated_at = now()
		WHERE id = $1
		RETURNING `+anomalyCols,
		id, rule.Name, rule.App, string(rule.Metric), rule.WindowMinutes, rule.BaselineWeeks,
		rule.Sensitivity, string(rule.Direction), rule.SilenceSeconds,
		string(rule.Channel), rule.TargetURL, rule.Active).
		Scan(&rule.ID, &rule.Name, &rule.App, &rule.Metric, &rule.WindowMinutes,
			&rule.BaselineWeeks, &rule.Sensitivity, &rule.Direction, &rule.SilenceSeconds,
			&rule.Channel, &rule.TargetURL, &rule.Active, &rule.CreatedAt, &rule.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AnomalyRule{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.AnomalyRule{}, fmt.Errorf("atualizando anomaly rule: %w", err)
	}
	return rule, nil
}

func (r *Repository) DeleteAnomalyRule(ctx context.Context, id string) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM anomaly_rules WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("removendo anomaly rule: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *Repository) ListAnomalyRules(ctx context.Context) ([]domain.AnomalyRule, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+anomalyCols+` FROM anomaly_rules ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("listando anomaly rules: %w", err)
	}
	defer rows.Close()
	out := []domain.AnomalyRule{}
	for rows.Next() {
		var r domain.AnomalyRule
		if err := rows.Scan(&r.ID, &r.Name, &r.App, &r.Metric, &r.WindowMinutes,
			&r.BaselineWeeks, &r.Sensitivity, &r.Direction, &r.SilenceSeconds,
			&r.Channel, &r.TargetURL, &r.Active, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LastDetection é usada para o silence window (dedup de disparos).
func (r *Repository) LastDetection(ctx context.Context, ruleID string) (domain.AnomalyDetection, error) {
	var d domain.AnomalyDetection
	err := r.pool.QueryRow(ctx, `
		SELECT id, rule_id::text, detected_at, current_value, baseline_avg,
		       baseline_stddev, z_score, direction, notify_ok, notify_detail
		FROM anomaly_detections
		WHERE rule_id = $1::uuid
		ORDER BY detected_at DESC LIMIT 1`, ruleID).
		Scan(&d.ID, &d.RuleID, &d.DetectedAt, &d.CurrentValue, &d.BaselineAvg,
			&d.BaselineStddev, &d.ZScore, &d.Direction, &d.NotifyOK, &d.NotifyDetail)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AnomalyDetection{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.AnomalyDetection{}, fmt.Errorf("last detection: %w", err)
	}
	return d, nil
}

func (r *Repository) RecordDetection(ctx context.Context, d domain.AnomalyDetection) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO anomaly_detections
			(rule_id, current_value, baseline_avg, baseline_stddev, z_score,
			 direction, notify_ok, notify_detail)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8)`,
		d.RuleID, d.CurrentValue, d.BaselineAvg, d.BaselineStddev,
		d.ZScore, string(d.Direction), d.NotifyOK, d.NotifyDetail)
	if err != nil {
		return fmt.Errorf("record detection: %w", err)
	}
	return nil
}

func (r *Repository) ListDetections(ctx context.Context, limit, offset int) ([]domain.AnomalyDetection, int, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM anomaly_detections`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT d.id, d.rule_id::text, r.name, d.detected_at, d.current_value,
		       d.baseline_avg, d.baseline_stddev, d.z_score, d.direction,
		       d.notify_ok, d.notify_detail
		FROM anomaly_detections d
		JOIN anomaly_rules r ON r.id = d.rule_id
		ORDER BY d.detected_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("listando detections: %w", err)
	}
	defer rows.Close()
	out := []domain.AnomalyDetection{}
	for rows.Next() {
		var d domain.AnomalyDetection
		if err := rows.Scan(&d.ID, &d.RuleID, &d.RuleName, &d.DetectedAt, &d.CurrentValue,
			&d.BaselineAvg, &d.BaselineStddev, &d.ZScore, &d.Direction,
			&d.NotifyOK, &d.NotifyDetail); err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	return out, total, rows.Err()
}

// ---------------------------------------------------------------------------
// SnapshotMetaStore (Sprint H) — session replay MVP
// ---------------------------------------------------------------------------

// CreateSnapshotMeta grava a metadata. INSERT ... ON CONFLICT DO NOTHING
// no event_id porque o SDK pode reenviar em caso de retry — evita duplicar
// sem erro.
func (r *Repository) CreateSnapshotMeta(ctx context.Context, s domain.SessionSnapshot) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO session_snapshots
			(event_id, session_id, app, object_key, size_bytes, viewport_w,
			 viewport_h, url, taken_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (event_id) DO NOTHING`,
		s.EventID, s.SessionID, s.App, s.ObjectKey, s.SizeBytes,
		s.ViewportW, s.ViewportH, s.URL, s.TakenAt)
	if err != nil {
		return fmt.Errorf("gravando snapshot meta: %w", err)
	}
	return nil
}

// GetSnapshotMetaByEvent busca por event_id (a UI aponta pra lá diretamente).
func (r *Repository) GetSnapshotMetaByEvent(ctx context.Context, eventID string) (domain.SessionSnapshot, error) {
	var s domain.SessionSnapshot
	err := r.pool.QueryRow(ctx, `
		SELECT id, event_id::text, session_id, app, object_key, size_bytes,
		       viewport_w, viewport_h, url, taken_at, received_at
		FROM session_snapshots WHERE event_id = $1::uuid`, eventID).
		Scan(&s.ID, &s.EventID, &s.SessionID, &s.App, &s.ObjectKey, &s.SizeBytes,
			&s.ViewportW, &s.ViewportH, &s.URL, &s.TakenAt, &s.ReceivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SessionSnapshot{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.SessionSnapshot{}, fmt.Errorf("consultando snapshot meta: %w", err)
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// SamplingRuleStore (Sprint G) — sampling adaptativo server-side
// ---------------------------------------------------------------------------

const samplingCols = `id, app, event_type, sample_rate, keep_errors, active,
	note, created_at, updated_at`

// CreateSamplingRule insere uma regra. Colisão em (app, event_type) vira ErrConflict.
func (r *Repository) CreateSamplingRule(ctx context.Context, rule domain.SamplingRule) (domain.SamplingRule, error) {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO sampling_rules (app, event_type, sample_rate, keep_errors, active, note)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+samplingCols,
		rule.App, rule.EventType, rule.SampleRate, rule.KeepErrors, rule.Active, rule.Note).
		Scan(&rule.ID, &rule.App, &rule.EventType, &rule.SampleRate, &rule.KeepErrors,
			&rule.Active, &rule.Note, &rule.CreatedAt, &rule.UpdatedAt)
	if isUniqueViolation(err) {
		return domain.SamplingRule{}, domain.ErrConflict
	}
	if err != nil {
		return domain.SamplingRule{}, fmt.Errorf("criando sampling rule: %w", err)
	}
	return rule, nil
}

// GetSamplingRule resolve uma regra por id (usado no ownership check).
func (r *Repository) GetSamplingRule(ctx context.Context, id string) (domain.SamplingRule, error) {
	var rule domain.SamplingRule
	err := r.pool.QueryRow(ctx,
		`SELECT `+samplingCols+` FROM sampling_rules WHERE id = $1`, id).
		Scan(&rule.ID, &rule.App, &rule.EventType, &rule.SampleRate, &rule.KeepErrors,
			&rule.Active, &rule.Note, &rule.CreatedAt, &rule.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SamplingRule{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.SamplingRule{}, fmt.Errorf("consultando sampling rule: %w", err)
	}
	return rule, nil
}

// UpdateSamplingRule atualiza uma regra.
func (r *Repository) UpdateSamplingRule(ctx context.Context, id string, rule domain.SamplingRule) (domain.SamplingRule, error) {
	err := r.pool.QueryRow(ctx, `
		UPDATE sampling_rules SET
			app = $2, event_type = $3, sample_rate = $4, keep_errors = $5,
			active = $6, note = $7, updated_at = now()
		WHERE id = $1
		RETURNING `+samplingCols,
		id, rule.App, rule.EventType, rule.SampleRate, rule.KeepErrors, rule.Active, rule.Note).
		Scan(&rule.ID, &rule.App, &rule.EventType, &rule.SampleRate, &rule.KeepErrors,
			&rule.Active, &rule.Note, &rule.CreatedAt, &rule.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SamplingRule{}, domain.ErrNotFound
	}
	if isUniqueViolation(err) {
		return domain.SamplingRule{}, domain.ErrConflict
	}
	if err != nil {
		return domain.SamplingRule{}, fmt.Errorf("atualizando sampling rule: %w", err)
	}
	return rule, nil
}

// DeleteSamplingRule remove por id.
func (r *Repository) DeleteSamplingRule(ctx context.Context, id string) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM sampling_rules WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("removendo sampling rule: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ListSamplingRules devolve TODAS as regras (ativas + inativas — a UI mostra
// as duas; a avaliação em runtime ignora inativas).
func (r *Repository) ListSamplingRules(ctx context.Context) ([]domain.SamplingRule, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+samplingCols+` FROM sampling_rules ORDER BY app, event_type`)
	if err != nil {
		return nil, fmt.Errorf("listando sampling rules: %w", err)
	}
	defer rows.Close()
	out := []domain.SamplingRule{}
	for rows.Next() {
		var r domain.SamplingRule
		if err := rows.Scan(&r.ID, &r.App, &r.EventType, &r.SampleRate, &r.KeepErrors,
			&r.Active, &r.Note, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// UserAppPermissionStore (Sprint E.4)
// ---------------------------------------------------------------------------

// GrantAppPermission concede acesso de um user a um app. Idempotente:
// reenvio atualiza role e granted_by. NÃO checa cross-company aqui — o
// caller (service) valida isso antes para dar mensagem clara.
func (r *Repository) GrantAppPermission(ctx context.Context, userID, appID, role, grantedBy string) (domain.UserAppPermission, error) {
	if role == "" {
		role = "viewer"
	}
	var p domain.UserAppPermission
	err := r.pool.QueryRow(ctx, `
		INSERT INTO user_app_permissions (user_id, app_id, role, granted_by)
		VALUES ($1::uuid, $2::uuid, $3, NULLIF($4, '')::uuid)
		ON CONFLICT (user_id, app_id) DO UPDATE
		  SET role = EXCLUDED.role, granted_by = EXCLUDED.granted_by, granted_at = now()
		RETURNING user_id::text, app_id::text, role, granted_at`,
		userID, appID, role, grantedBy).
		Scan(&p.UserID, &p.AppID, &p.Role, &p.GrantedAt)
	if isForeignKeyViolation(err) {
		return domain.UserAppPermission{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.UserAppPermission{}, fmt.Errorf("grant permission: %w", err)
	}
	return p, nil
}

// RevokeAppPermission remove a permissão. Silenciosamente 404 se não existir.
func (r *Repository) RevokeAppPermission(ctx context.Context, userID, appID string) error {
	ct, err := r.pool.Exec(ctx,
		`DELETE FROM user_app_permissions WHERE user_id = $1::uuid AND app_id = $2::uuid`,
		userID, appID)
	if err != nil {
		return fmt.Errorf("revoke permission: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ListAppPermissionsForUser devolve todas as concessões de um user, com o
// nome do app resolvido via join (a UI mostra sem query adicional).
func (r *Repository) ListAppPermissionsForUser(ctx context.Context, userID string) ([]domain.UserAppPermission, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT p.user_id::text, p.app_id::text, a.name, p.role, p.granted_at
		FROM user_app_permissions p
		JOIN apps a ON a.id = p.app_id
		WHERE p.user_id = $1::uuid
		ORDER BY a.name`, userID)
	if err != nil {
		return nil, fmt.Errorf("listando permissions: %w", err)
	}
	defer rows.Close()
	out := []domain.UserAppPermission{}
	for rows.Next() {
		var p domain.UserAppPermission
		if err := rows.Scan(&p.UserID, &p.AppID, &p.AppName, &p.Role, &p.GrantedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListAppNamesForUser devolve os nomes dos apps que o user tem acesso
// explícito — usado pelo tenantScope middleware para viewers.
func (r *Repository) ListAppNamesForUser(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT a.name FROM user_app_permissions p
		JOIN apps a ON a.id = p.app_id
		WHERE p.user_id = $1::uuid`, userID)
	if err != nil {
		return nil, fmt.Errorf("app names por user: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// AuditStore (Sprint E.1) — insert-only, sem update/delete por design
// ---------------------------------------------------------------------------

// RecordAudit insere uma entrada. Falha aqui NÃO deve travar a mutação
// original (o caller loga warn e segue) — auditoria é best-effort quando o
// insert falha, embora normalmente o Postgres esteja disponível.
func (r *Repository) RecordAudit(ctx context.Context, e domain.AuditEntry) error {
	details := e.Details
	if len(details) == 0 {
		details = []byte("{}")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO audit_log (actor_user_id, actor_email, action, resource_type,
			resource_id, details, ip, user_agent)
		VALUES (NULLIF($1, '')::uuid, $2, $3, $4, $5, $6::jsonb, $7, $8)`,
		e.ActorUserID, e.ActorEmail, e.Action, e.ResourceType,
		e.ResourceID, string(details), e.IP, e.UserAgent)
	if err != nil {
		return fmt.Errorf("gravando audit: %w", err)
	}
	return nil
}

// ListAudit devolve entradas paginadas + total (para paginação da UI).
// Filtros são cumulativos (AND).
func (r *Repository) ListAudit(ctx context.Context, f domain.AuditFilter) ([]domain.AuditEntry, int, error) {
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}

	// Query dinâmica com placeholders posicionais.
	where := []string{"1=1"}
	args := []any{}
	add := func(clause string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if f.Actor != "" {
		add("a.actor_user_id = $%d::uuid", f.Actor)
	}
	if f.Action != "" {
		add("a.action = $%d", f.Action)
	}
	if f.ResourceType != "" {
		add("a.resource_type = $%d", f.ResourceType)
	}
	if f.From != nil {
		add("a.created_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("a.created_at < $%d", *f.To)
	}
	// Filtro por company do actor (usa JOIN opcional em users).
	if f.CompanyID != "" {
		add("EXISTS (SELECT 1 FROM users u WHERE u.id = a.actor_user_id AND u.company_id = $%d::uuid)", f.CompanyID)
	}

	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := r.pool.QueryRow(ctx,
		"SELECT count(*) FROM audit_log a WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("contando audit: %w", err)
	}

	args = append(args, limit, f.Offset)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT a.id, COALESCE(a.actor_user_id::text, ''), a.actor_email, a.action, a.resource_type,
		       a.resource_id, a.details, a.ip, a.user_agent, a.created_at
		FROM audit_log a WHERE %s
		ORDER BY a.created_at DESC
		LIMIT $%d OFFSET $%d`, whereSQL, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("consultando audit: %w", err)
	}
	defer rows.Close()

	out := []domain.AuditEntry{}
	for rows.Next() {
		var e domain.AuditEntry
		if err := rows.Scan(&e.ID, &e.ActorUserID, &e.ActorEmail, &e.Action,
			&e.ResourceType, &e.ResourceID, &e.Details, &e.IP, &e.UserAgent, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// ---------------------------------------------------------------------------
// FunnelStore (Sprint C.2)
// ---------------------------------------------------------------------------

const funnelColumns = `id, app, name, window_seconds, steps,
	COALESCE(created_by::text, ''), created_at, updated_at`

// CreateFunnel insere um funil. Nome duplicado no mesmo app vira ErrConflict.
func (r *Repository) CreateFunnel(ctx context.Context, f domain.Funnel) (domain.Funnel, error) {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO funnels (app, name, window_seconds, steps, created_by)
		VALUES ($1, $2, $3, $4::jsonb, NULLIF($5, '')::uuid)
		RETURNING `+funnelColumns,
		f.App, f.Name, f.WindowSeconds, string(f.Steps), f.CreatedBy).
		Scan(&f.ID, &f.App, &f.Name, &f.WindowSeconds, &f.Steps, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt)
	if isUniqueViolation(err) {
		return domain.Funnel{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Funnel{}, fmt.Errorf("criando funnel: %w", err)
	}
	return f, nil
}

// UpdateFunnel edita nome/janela/steps. Não deixa trocar o app (funil vive por app).
func (r *Repository) UpdateFunnel(ctx context.Context, id string, name string, windowSeconds int, steps json.RawMessage) (domain.Funnel, error) {
	var f domain.Funnel
	err := r.pool.QueryRow(ctx, `
		UPDATE funnels
		SET name = $2, window_seconds = $3, steps = $4::jsonb, updated_at = now()
		WHERE id = $1
		RETURNING `+funnelColumns,
		id, name, windowSeconds, string(steps)).
		Scan(&f.ID, &f.App, &f.Name, &f.WindowSeconds, &f.Steps, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Funnel{}, domain.ErrNotFound
	}
	if isUniqueViolation(err) {
		return domain.Funnel{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Funnel{}, fmt.Errorf("atualizando funnel: %w", err)
	}
	return f, nil
}

// ListFunnels lista os funis, opcionalmente filtrados por app.
func (r *Repository) ListFunnels(ctx context.Context, app string) ([]domain.Funnel, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+funnelColumns+` FROM funnels WHERE $1 = '' OR app = $1 ORDER BY app, name`,
		app)
	if err != nil {
		return nil, fmt.Errorf("listando funnels: %w", err)
	}
	defer rows.Close()
	out := []domain.Funnel{}
	for rows.Next() {
		var f domain.Funnel
		if err := rows.Scan(&f.ID, &f.App, &f.Name, &f.WindowSeconds, &f.Steps,
			&f.CreatedBy, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetFunnel retorna um funil por id.
func (r *Repository) GetFunnel(ctx context.Context, id string) (domain.Funnel, error) {
	var f domain.Funnel
	err := r.pool.QueryRow(ctx, `SELECT `+funnelColumns+` FROM funnels WHERE id = $1`, id).
		Scan(&f.ID, &f.App, &f.Name, &f.WindowSeconds, &f.Steps, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Funnel{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Funnel{}, fmt.Errorf("consultando funnel: %w", err)
	}
	return f, nil
}

// DeleteFunnel remove por id.
func (r *Repository) DeleteFunnel(ctx context.Context, id string) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM funnels WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("removendo funnel: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// SavedViewStore (Sprint C.1)
// ---------------------------------------------------------------------------

const savedViewColumns = `v.id, COALESCE(v.owner_user_id::text, ''),
	COALESCE(u.name, ''), v.view_type, v.name, v.filters, v.is_shared,
	v.created_at, v.updated_at`

// CreateSavedView insere uma view. Nome duplicado (mesmo dono + tipo) vira ErrConflict.
func (r *Repository) CreateSavedView(ctx context.Context, v domain.SavedView) (domain.SavedView, error) {
	if v.OwnerUserID == "" {
		return domain.SavedView{}, domain.NewValidationError("ownerUserId é obrigatório")
	}
	filters := v.Filters
	if len(filters) == 0 {
		filters = []byte("{}")
	}
	err := r.pool.QueryRow(ctx, `
		WITH ins AS (
			INSERT INTO saved_views (owner_user_id, view_type, name, filters, is_shared)
			VALUES ($1::uuid, $2, $3, $4::jsonb, $5)
			RETURNING id, owner_user_id, view_type, name, filters, is_shared, created_at, updated_at
		)
		SELECT `+savedViewColumns+`
		FROM ins v LEFT JOIN users u ON u.id = v.owner_user_id`,
		v.OwnerUserID, v.ViewType, v.Name, string(filters), v.IsShared).
		Scan(&v.ID, &v.OwnerUserID, &v.OwnerName, &v.ViewType, &v.Name, &v.Filters,
			&v.IsShared, &v.CreatedAt, &v.UpdatedAt)
	if isUniqueViolation(err) {
		return domain.SavedView{}, domain.ErrConflict
	}
	if err != nil {
		return domain.SavedView{}, fmt.Errorf("criando saved view: %w", err)
	}
	return v, nil
}

// UpdateSavedView só permite ao dono editar. Retorna ErrNotFound se o id não
// existe OU se o requester não é o dono — evita revelar existência.
func (r *Repository) UpdateSavedView(ctx context.Context, id, ownerID, name string, filters json.RawMessage, isShared bool) (domain.SavedView, error) {
	if len(filters) == 0 {
		filters = []byte("{}")
	}
	var v domain.SavedView
	err := r.pool.QueryRow(ctx, `
		WITH upd AS (
			UPDATE saved_views
			SET name = $3, filters = $4::jsonb, is_shared = $5, updated_at = now()
			WHERE id = $1 AND owner_user_id = $2::uuid
			RETURNING id, owner_user_id, view_type, name, filters, is_shared, created_at, updated_at
		)
		SELECT `+savedViewColumns+`
		FROM upd v LEFT JOIN users u ON u.id = v.owner_user_id`,
		id, ownerID, name, string(filters), isShared).
		Scan(&v.ID, &v.OwnerUserID, &v.OwnerName, &v.ViewType, &v.Name, &v.Filters,
			&v.IsShared, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SavedView{}, domain.ErrNotFound
	}
	if isUniqueViolation(err) {
		return domain.SavedView{}, domain.ErrConflict
	}
	if err != nil {
		return domain.SavedView{}, fmt.Errorf("atualizando saved view: %w", err)
	}
	return v, nil
}

// DeleteSavedView só o dono pode apagar.
func (r *Repository) DeleteSavedView(ctx context.Context, id, ownerID string) error {
	ct, err := r.pool.Exec(ctx,
		`DELETE FROM saved_views WHERE id = $1 AND owner_user_id = $2::uuid`, id, ownerID)
	if err != nil {
		return fmt.Errorf("removendo saved view: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ListSavedViews retorna as views visíveis para o usuário: as próprias +
// as compartilhadas por outros usuários no mesmo tipo.
func (r *Repository) ListSavedViews(ctx context.Context, ownerID, viewType string) ([]domain.SavedView, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+savedViewColumns+`
		FROM saved_views v LEFT JOIN users u ON u.id = v.owner_user_id
		WHERE ($1 = '' OR v.view_type = $1)
		  AND (v.owner_user_id = NULLIF($2, '')::uuid OR v.is_shared)
		ORDER BY v.is_shared, u.name NULLS FIRST, v.name`,
		viewType, ownerID)
	if err != nil {
		return nil, fmt.Errorf("listando saved views: %w", err)
	}
	defer rows.Close()
	out := []domain.SavedView{}
	for rows.Next() {
		var v domain.SavedView
		if err := rows.Scan(&v.ID, &v.OwnerUserID, &v.OwnerName, &v.ViewType, &v.Name,
			&v.Filters, &v.IsShared, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// SourceMapStore
// ---------------------------------------------------------------------------

const sourceMapColumns = `id, app, release, filename, size_bytes,
	COALESCE(uploaded_by::text, '') AS uploaded_by, uploaded_at`

// UpsertSourceMap insere ou atualiza um source map (chave: app+release+filename).
// Reenvio após rebuild sobrescreve o conteúdo — não guardamos histórico
// porque o valor útil está sempre na versão mais recente daquele build.
func (r *Repository) UpsertSourceMap(ctx context.Context, m domain.SourceMap, content string) (domain.SourceMap, error) {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO source_maps (app, release, filename, content, size_bytes, uploaded_by)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::uuid)
		ON CONFLICT (app, release, filename) DO UPDATE
		  SET content = EXCLUDED.content,
		      size_bytes = EXCLUDED.size_bytes,
		      uploaded_by = EXCLUDED.uploaded_by,
		      uploaded_at = now()
		RETURNING `+sourceMapColumns,
		m.App, m.Release, m.Filename, content, len(content), m.UploadedBy).
		Scan(&m.ID, &m.App, &m.Release, &m.Filename, &m.SizeBytes, &m.UploadedBy, &m.UploadedAt)
	if err != nil {
		return domain.SourceMap{}, fmt.Errorf("upsert source map: %w", err)
	}
	return m, nil
}

// GetSourceMap resolve um source map por id (usado no ownership check).
func (r *Repository) GetSourceMap(ctx context.Context, id string) (domain.SourceMap, error) {
	var m domain.SourceMap
	err := r.pool.QueryRow(ctx,
		`SELECT `+sourceMapColumns+` FROM source_maps WHERE id = $1`, id).
		Scan(&m.ID, &m.App, &m.Release, &m.Filename, &m.SizeBytes, &m.UploadedBy, &m.UploadedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SourceMap{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.SourceMap{}, fmt.Errorf("consultando source map: %w", err)
	}
	return m, nil
}

// ListSourceMaps lista metadados (sem o content). Filtro opcional por app/release.
func (r *Repository) ListSourceMaps(ctx context.Context, app, release string) ([]domain.SourceMap, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+sourceMapColumns+` FROM source_maps
		 WHERE ($1 = '' OR app = $1) AND ($2 = '' OR release = $2)
		 ORDER BY uploaded_at DESC`, app, release)
	if err != nil {
		return nil, fmt.Errorf("listando source maps: %w", err)
	}
	defer rows.Close()
	out := []domain.SourceMap{}
	for rows.Next() {
		var m domain.SourceMap
		if err := rows.Scan(&m.ID, &m.App, &m.Release, &m.Filename, &m.SizeBytes,
			&m.UploadedBy, &m.UploadedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetSourceMapContent carrega o conteúdo bruto do .map para o serviço de
// resolução de stack. Retorna ErrNotFound se não existir.
func (r *Repository) GetSourceMapContent(ctx context.Context, app, release, filename string) (string, error) {
	var content string
	err := r.pool.QueryRow(ctx,
		`SELECT content FROM source_maps WHERE app = $1 AND release = $2 AND filename = $3`,
		app, release, filename).Scan(&content)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("consultando source map: %w", err)
	}
	return content, nil
}

// DeleteSourceMap remove por id.
func (r *Repository) DeleteSourceMap(ctx context.Context, id string) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM source_maps WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("removendo source map: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func roleArg(r *domain.Role) *string {
	if r == nil {
		return nil
	}
	s := string(*r)
	return &s
}

func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
