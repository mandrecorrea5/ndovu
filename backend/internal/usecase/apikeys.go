package usecase

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// keyPrefixLen é quantos caracteres da chave ficam visíveis para identificação.
const keyPrefixLen = 12

// APIKeyService gerencia as chaves de ingestão e valida chaves apresentadas
// pelos frontends. A validação usa cache em memória com TTL curto: a ingestão
// continua respondendo em ~1ms sem uma ida ao Postgres por lote.
//
// Também expõe um rate limiter por chave (token bucket): mesmo com uma chave
// vazada, um cliente não consegue derrubar a ingestão dos outros apps.
type APIKeyService struct {
	store    domain.APIKeyStore
	logger   *slog.Logger
	cacheTTL time.Duration
	keyAEAD  cipher.AEAD

	mu    sync.RWMutex
	cache map[string]cachedKey // key: hash da chave

	// Rate limit: N requests/s por chave, burst = 2N.
	rateRPS    float64
	limiters   map[string]*rate.Limiter // key: chave ID
	limitersMu sync.Mutex
}

type cachedKey struct {
	key       domain.APIKey
	valid     bool
	expiresAt time.Time
}

// NewAPIKeyService cria o serviço de chaves. rateRPS <= 0 desliga o rate limit.
func NewAPIKeyService(store domain.APIKeyStore, cacheTTL time.Duration, rateRPS float64, logger *slog.Logger, encryptionSecret string) *APIKeyService {
	service := &APIKeyService{
		store:    store,
		logger:   logger,
		cacheTTL: cacheTTL,
		cache:    map[string]cachedKey{},
		rateRPS:  rateRPS,
		limiters: map[string]*rate.Limiter{},
	}
	if encryptionSecret != "" {
		key := sha256.Sum256([]byte("ndovu/api-key-encryption/v1\x00" + encryptionSecret))
		block, err := aes.NewCipher(key[:])
		if err == nil {
			service.keyAEAD, err = cipher.NewGCM(block)
			if err != nil {
				service.logger.Error("inicializando cifra das chaves de API", "err", err)
			}
		}
	}
	return service
}

// Allow retorna false quando a chave estourou o rate limit — a ingestão
// devolve 429 com Retry-After. Se rateRPS <= 0, sempre permite (desligado).
func (s *APIKeyService) Allow(key domain.APIKey) bool {
	if s.rateRPS <= 0 {
		return true
	}
	s.limitersMu.Lock()
	lim, ok := s.limiters[key.ID]
	if !ok {
		lim = rate.NewLimiter(rate.Limit(s.rateRPS), int(s.rateRPS*2)+1)
		s.limiters[key.ID] = lim
	}
	s.limitersMu.Unlock()
	return lim.Allow()
}

// CreatedKey retorna a chave recém-criada em claro.
type CreatedKey struct {
	domain.APIKey
}

// CreateKey gera uma chave nova para um app ("ndk_" + 32 bytes hex).
func (s *APIKeyService) CreateKey(ctx context.Context, appID, app, label, createdBy string) (CreatedKey, error) {
	if app == "" {
		return CreatedKey{}, domain.NewValidationError("app é obrigatório")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return CreatedKey{}, fmt.Errorf("gerando chave: %w", err)
	}
	plaintext := "ndk_" + hex.EncodeToString(raw)
	if s.keyAEAD == nil {
		return CreatedKey{}, errors.New("cifragem de chaves de API não configurada")
	}
	nonce := make([]byte, s.keyAEAD.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return CreatedKey{}, fmt.Errorf("gerando nonce para chave: %w", err)
	}
	ciphertext := s.keyAEAD.Seal(nonce, nonce, []byte(plaintext), []byte(appID))

	key, err := s.store.CreateAPIKey(ctx, domain.APIKey{
		AppID:        appID,
		App:          app,
		Label:        label,
		Prefix:       plaintext[:keyPrefixLen],
		EncryptedKey: base64.RawURLEncoding.EncodeToString(ciphertext),
	}, hashKey(plaintext), createdBy)
	if err != nil {
		return CreatedKey{}, err
	}
	s.clearValidationCache()
	s.logger.InfoContext(ctx, "chave de ingestão criada", "app", app, "prefix", key.Prefix)
	key.Key = plaintext
	return CreatedKey{APIKey: key}, nil
}

// ListKeys lista as chaves (visão global — usar só para super-admin).
func (s *APIKeyService) ListKeys(ctx context.Context) ([]domain.APIKey, error) {
	keys, err := s.store.ListAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	return s.revealKeys(keys)
}

// ListKeysByCompany filtra por company — usar para admin não-super.
func (s *APIKeyService) ListKeysByCompany(ctx context.Context, companyID string) ([]domain.APIKey, error) {
	keys, err := s.store.ListAPIKeysByCompany(ctx, companyID)
	if err != nil {
		return nil, err
	}
	return s.revealKeys(keys)
}

func (s *APIKeyService) revealKeys(keys []domain.APIKey) ([]domain.APIKey, error) {
	for i := range keys {
		if keys[i].EncryptedKey == "" {
			continue
		}
		if s.keyAEAD == nil {
			return nil, errors.New("cifragem de chaves de API não configurada")
		}
		ciphertext, err := base64.RawURLEncoding.DecodeString(keys[i].EncryptedKey)
		if err != nil {
			return nil, fmt.Errorf("decodificando chave %s: %w", keys[i].ID, err)
		}
		if len(ciphertext) < s.keyAEAD.NonceSize() {
			return nil, fmt.Errorf("chave cifrada inválida: %s", keys[i].ID)
		}
		nonce, ciphertext := ciphertext[:s.keyAEAD.NonceSize()], ciphertext[s.keyAEAD.NonceSize():]
		plaintext, err := s.keyAEAD.Open(nil, nonce, ciphertext, []byte(keys[i].AppID))
		if err != nil {
			return nil, fmt.Errorf("decifrando chave %s: %w", keys[i].ID, err)
		}
		keys[i].Key = string(plaintext)
	}
	return keys, nil
}

// GetKeyByID resolve uma chave pelo id — usado no ownership check antes
// de revogar.
func (s *APIKeyService) GetKeyByID(ctx context.Context, id string) (domain.APIKey, error) {
	return s.store.GetAPIKeyByID(ctx, id)
}

// RevokeKey desativa uma chave e invalida o cache imediatamente.
func (s *APIKeyService) RevokeKey(ctx context.Context, id string) error {
	if err := s.store.RevokeAPIKey(ctx, id); err != nil {
		return err
	}
	s.clearValidationCache()
	s.logger.InfoContext(ctx, "chave de ingestão revogada", "id", id)
	return nil
}

func (s *APIKeyService) clearValidationCache() {
	s.mu.Lock()
	s.cache = map[string]cachedKey{}
	s.mu.Unlock()
}

// RevokeByApp revoga todas as chaves ativas de um app (usado ao excluir o app).
func (s *APIKeyService) RevokeByApp(ctx context.Context, appID string) error {
	keys, err := s.store.ListAPIKeys(ctx)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if k.AppID == appID && k.Active {
			if err := s.store.RevokeAPIKey(ctx, k.ID); err != nil {
				return err
			}
		}
	}
	s.clearValidationCache()
	return nil
}

// Validate verifica a chave apresentada em X-Api-Key. Retorna a chave (com o
// app dono) quando válida; domain.ErrUnauthorized caso contrário.
func (s *APIKeyService) Validate(ctx context.Context, presented string) (domain.APIKey, error) {
	if presented == "" {
		return domain.APIKey{}, domain.ErrUnauthorized
	}
	hash := hashKey(presented)

	s.mu.RLock()
	entry, ok := s.cache[hash]
	s.mu.RUnlock()
	if ok && time.Now().Before(entry.expiresAt) {
		if !entry.valid {
			return domain.APIKey{}, domain.ErrUnauthorized
		}
		return entry.key, nil
	}

	key, err := s.store.FindActiveKeyByHash(ctx, hash)
	switch {
	case err == nil:
		s.storeCache(hash, cachedKey{key: key, valid: true, expiresAt: time.Now().Add(s.cacheTTL)})
		return key, nil
	case errors.Is(err, domain.ErrNotFound):
		// Cache negativo evita que chave inválida martele o banco.
		s.storeCache(hash, cachedKey{valid: false, expiresAt: time.Now().Add(s.cacheTTL)})
		return domain.APIKey{}, domain.ErrUnauthorized
	default:
		return domain.APIKey{}, fmt.Errorf("validando chave: %w", err)
	}
}

// EnsureBootstrapKey garante uma chave inicial de desenvolvimento quando não
// existe nenhuma — para o compose e o seed funcionarem de primeira.
func (s *APIKeyService) EnsureBootstrapKey(ctx context.Context, plaintext, app string) error {
	if plaintext == "" {
		return nil
	}
	keys, err := s.store.ListAPIKeys(ctx)
	if err != nil {
		return fmt.Errorf("verificando chaves existentes: %w", err)
	}
	if len(keys) > 0 {
		return nil
	}
	prefix := plaintext
	if len(prefix) > keyPrefixLen {
		prefix = prefix[:keyPrefixLen]
	}
	_, err = s.store.CreateAPIKey(ctx, domain.APIKey{
		App:    app,
		Label:  "chave bootstrap (troque em produção)",
		Prefix: prefix,
	}, hashKey(plaintext), "")
	if err != nil && !errors.Is(err, domain.ErrConflict) {
		return fmt.Errorf("criando chave bootstrap: %w", err)
	}
	s.logger.Info("chave de ingestão bootstrap criada", "app", app)
	return nil
}

func (s *APIKeyService) storeCache(hash string, entry cachedKey) {
	s.mu.Lock()
	s.cache[hash] = entry
	s.mu.Unlock()
}

func hashKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
