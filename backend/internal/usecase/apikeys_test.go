package usecase

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// fakeKeyStore para os testes de APIKeyService. Diferente do fakeKeyStore
// de auth_test.go (que testa AuthService via CreateUser paths) — este simula
// o ciclo de vida completo da chave: hash lookup, revoke, list.
type fakeAPIKeyStore struct {
	mu       sync.Mutex
	keys     []domain.APIKey
	hashes   map[string]string // keyID -> hash (pra lookup)
	seq      int
	failList error // para simular erro em ListAPIKeys
}

func newFakeAPIKeyStore() *fakeAPIKeyStore {
	return &fakeAPIKeyStore{hashes: map[string]string{}}
}

func (f *fakeAPIKeyStore) CreateAPIKey(_ context.Context, k domain.APIKey, hash, _ string) (domain.APIKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	k.ID = string(rune('A' + f.seq))
	k.Active = true
	k.CreatedAt = time.Now()
	f.keys = append(f.keys, k)
	f.hashes[k.ID] = hash
	return k, nil
}

func (f *fakeAPIKeyStore) ListAPIKeys(_ context.Context) ([]domain.APIKey, error) {
	if f.failList != nil {
		return nil, f.failList
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]domain.APIKey{}, f.keys...)
	return out, nil
}

func (f *fakeAPIKeyStore) ListAPIKeysByCompany(_ context.Context, _ string) ([]domain.APIKey, error) {
	return f.ListAPIKeys(context.Background())
}

func (f *fakeAPIKeyStore) GetAPIKeyByID(_ context.Context, id string) (domain.APIKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range f.keys {
		if k.ID == id {
			return k, nil
		}
	}
	return domain.APIKey{}, domain.ErrNotFound
}

func (f *fakeAPIKeyStore) RevokeAPIKey(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, k := range f.keys {
		if k.ID == id {
			f.keys[i].Active = false
			return nil
		}
	}
	return domain.ErrNotFound
}

func (f *fakeAPIKeyStore) FindActiveKeyByHash(_ context.Context, hash string) (domain.APIKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range f.keys {
		if !k.Active {
			continue
		}
		if f.hashes[k.ID] == hash {
			return k, nil
		}
	}
	return domain.APIKey{}, domain.ErrNotFound
}

func newAPIKeySvc(rateRPS float64) (*APIKeyService, *fakeAPIKeyStore) {
	store := newFakeAPIKeyStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewAPIKeyService(store, 100*time.Millisecond, rateRPS, logger), store
}

func TestAPIKey_CreateGeraPrefixoNdk(t *testing.T) {
	svc, store := newAPIKeySvc(0)
	created, err := svc.CreateKey(context.Background(), "app-1", "portal-a", "prod", "actor")
	if err != nil {
		t.Fatalf("erro criando: %v", err)
	}
	if !strings.HasPrefix(created.Key, "ndk_") {
		t.Errorf("chave deveria começar com 'ndk_', veio %q", created.Key)
	}
	// O prefix salvo (visível) deve ter keyPrefixLen chars.
	if len(created.Prefix) != keyPrefixLen {
		t.Errorf("prefix esperado %d chars, veio %d", keyPrefixLen, len(created.Prefix))
	}
	// A chave não deve ser guardada em claro no store — só hash.
	if len(store.keys) != 1 {
		t.Fatalf("store deveria ter 1 chave, veio %d", len(store.keys))
	}
	// Hash é sha256 hex (64 chars) — nunca a chave em claro.
	h := store.hashes[created.ID]
	if len(h) != 64 || h == created.Key {
		t.Errorf("hash mal-formado ou é a chave em claro: %q", h)
	}
}

func TestAPIKey_CreateExigeApp(t *testing.T) {
	svc, _ := newAPIKeySvc(0)
	_, err := svc.CreateKey(context.Background(), "", "", "prod", "actor")
	if err == nil {
		t.Fatal("esperava erro por app vazio")
	}
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("esperava ValidationError, veio %T", err)
	}
}

func TestAPIKey_ValidateChaveVaziaRejeita(t *testing.T) {
	svc, _ := newAPIKeySvc(0)
	_, err := svc.Validate(context.Background(), "")
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("chave vazia deveria ser ErrUnauthorized, veio %v", err)
	}
}

func TestAPIKey_ValidateHappyPathECache(t *testing.T) {
	svc, store := newAPIKeySvc(0)
	created, _ := svc.CreateKey(context.Background(), "app-1", "portal-a", "prod", "actor")

	// 1a validação: bate no store.
	key, err := svc.Validate(context.Background(), created.Key)
	if err != nil {
		t.Fatalf("validate falhou: %v", err)
	}
	if key.ID != created.ID {
		t.Errorf("chave errada retornada: %+v", key)
	}

	// Marca no store que list vai começar a falhar — se cache funciona,
	// a próxima validação nem toca no store.
	store.failList = errors.New("nunca deveria ser chamado")

	// 2a validação: vem do cache.
	_, err = svc.Validate(context.Background(), created.Key)
	if err != nil {
		t.Errorf("cache não funcionou: %v", err)
	}
}

func TestAPIKey_ValidateChaveInvalidaCacheNegativo(t *testing.T) {
	// Chave inválida também é cacheada (evita que ataque martele o banco).
	svc, _ := newAPIKeySvc(0)
	_, err := svc.Validate(context.Background(), "ndk_chave-que-nao-existe")
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("esperava Unauthorized, veio %v", err)
	}
	// 2a tentativa idêntica deve devolver o mesmo resultado (do cache negativo).
	_, err = svc.Validate(context.Background(), "ndk_chave-que-nao-existe")
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("cache negativo não funcionou: %v", err)
	}
}

func TestAPIKey_RevokeInvalidaCacheImediatamente(t *testing.T) {
	svc, _ := newAPIKeySvc(0)
	created, _ := svc.CreateKey(context.Background(), "app-1", "portal-a", "prod", "actor")
	// Warm cache
	if _, err := svc.Validate(context.Background(), created.Key); err != nil {
		t.Fatalf("validate 1a falhou: %v", err)
	}
	// Revoga
	if err := svc.RevokeKey(context.Background(), created.ID); err != nil {
		t.Fatalf("revoke falhou: %v", err)
	}
	// Próxima validação deve rejeitar imediatamente (cache foi limpo pelo revoke).
	if _, err := svc.Validate(context.Background(), created.Key); !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("chave revogada deveria ser rejeitada de imediato, veio %v", err)
	}
}

func TestAPIKey_RevokeByAppLimpaSoAquelaApp(t *testing.T) {
	svc, _ := newAPIKeySvc(0)
	ctx := context.Background()
	k1, _ := svc.CreateKey(ctx, "app-1", "portal-a", "", "actor")
	k2, _ := svc.CreateKey(ctx, "app-2", "portal-b", "", "actor")

	if err := svc.RevokeByApp(ctx, "app-1"); err != nil {
		t.Fatalf("revokeByApp falhou: %v", err)
	}
	// k1 revogada, k2 ainda ativa.
	if _, err := svc.Validate(ctx, k1.Key); !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("k1 deveria estar revogada")
	}
	if _, err := svc.Validate(ctx, k2.Key); err != nil {
		t.Errorf("k2 não deveria ter sido revogada: %v", err)
	}
}

func TestAPIKey_AllowSemRateLimitSempreLibera(t *testing.T) {
	svc, _ := newAPIKeySvc(0) // rateRPS=0 = desligado
	key := domain.APIKey{ID: "any"}
	for i := 0; i < 1000; i++ {
		if !svc.Allow(key) {
			t.Fatalf("Allow deveria sempre permitir sem rate limit, falhou na iteração %d", i)
		}
	}
}

func TestAPIKey_AllowComRateLimitBloqueiaBurst(t *testing.T) {
	// rate=10 req/s → burst=21. Um loop rápido de 100 tentativas deve
	// bater no limite em algum momento.
	svc, _ := newAPIKeySvc(10)
	key := domain.APIKey{ID: "hot-key"}
	blocked := 0
	for i := 0; i < 100; i++ {
		if !svc.Allow(key) {
			blocked++
		}
	}
	if blocked == 0 {
		t.Error("rate limit deveria ter bloqueado alguma request no burst")
	}
}

func TestAPIKey_EnsureBootstrapKeyNaoRecriaSeJaExiste(t *testing.T) {
	svc, store := newAPIKeySvc(0)
	// Cria uma chave manualmente.
	_, _ = svc.CreateKey(context.Background(), "app-1", "portal-a", "", "actor")
	countBefore := len(store.keys)

	// EnsureBootstrapKey em um sistema já com chaves não deve fazer nada.
	if err := svc.EnsureBootstrapKey(context.Background(), "dev-key", "dev"); err != nil {
		t.Fatalf("erro: %v", err)
	}
	if len(store.keys) != countBefore {
		t.Errorf("bootstrap não deveria criar nova chave; antes=%d depois=%d", countBefore, len(store.keys))
	}
}

func TestAPIKey_EnsureBootstrapKeyCriaSeVazio(t *testing.T) {
	svc, store := newAPIKeySvc(0)
	if err := svc.EnsureBootstrapKey(context.Background(), "dev-key", "dev"); err != nil {
		t.Fatalf("erro: %v", err)
	}
	if len(store.keys) != 1 {
		t.Errorf("esperava 1 chave bootstrap, veio %d", len(store.keys))
	}
}

func TestAPIKey_EnsureBootstrapKeyPlaintextVazioNoOp(t *testing.T) {
	svc, store := newAPIKeySvc(0)
	if err := svc.EnsureBootstrapKey(context.Background(), "", "dev"); err != nil {
		t.Fatalf("erro: %v", err)
	}
	if len(store.keys) != 0 {
		t.Errorf("plaintext vazio não deveria criar chave")
	}
}
