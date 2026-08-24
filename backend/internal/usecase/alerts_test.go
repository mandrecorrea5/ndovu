package usecase

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

type fakeAlertStore struct {
	rules   map[string]domain.AlertRule
	seq     int
	last    map[string]time.Time
	records []deliveryRecord
}

type deliveryRecord struct {
	ruleID string
	count  int
	ok     bool
	detail string
}

func newFakeAlertStore() *fakeAlertStore {
	return &fakeAlertStore{
		rules: map[string]domain.AlertRule{},
		last:  map[string]time.Time{},
	}
}

func (f *fakeAlertStore) CreateAlertRule(_ context.Context, r domain.AlertRule) (domain.AlertRule, error) {
	f.seq++
	r.ID = string(rune('A' + f.seq))
	f.rules[r.ID] = r
	return r, nil
}
func (f *fakeAlertStore) GetAlertRule(_ context.Context, id string) (domain.AlertRule, error) {
	if r, ok := f.rules[id]; ok {
		return r, nil
	}
	return domain.AlertRule{}, domain.ErrNotFound
}
func (f *fakeAlertStore) ListAlertRules(_ context.Context) ([]domain.AlertRule, error) {
	out := []domain.AlertRule{}
	for _, r := range f.rules {
		out = append(out, r)
	}
	return out, nil
}
func (f *fakeAlertStore) DeleteAlertRule(_ context.Context, id string) error {
	if _, ok := f.rules[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.rules, id)
	return nil
}
func (f *fakeAlertStore) LastDeliveryAt(_ context.Context, ruleID string) (time.Time, error) {
	return f.last[ruleID], nil
}
func (f *fakeAlertStore) RecordDelivery(_ context.Context, ruleID string, count int, ok bool, detail string) error {
	f.records = append(f.records, deliveryRecord{ruleID, count, ok, detail})
	f.last[ruleID] = time.Now()
	return nil
}

func newAlertSvc() (*AlertService, *fakeAlertStore) {
	store := newFakeAlertStore()
	reader := nopEventReader{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewAlertService(store, reader, logger), store
}

func TestAlert_CreateValida(t *testing.T) {
	svc, _ := newAlertSvc()
	cases := []struct {
		name string
		rule domain.AlertRule
	}{
		{"name vazio", domain.AlertRule{
			Threshold: 10, WindowSeconds: 300, Channel: domain.AlertChannelSlack, TargetURL: "http://x",
		}},
		{"threshold zero", domain.AlertRule{
			Name: "x", WindowSeconds: 300, Channel: domain.AlertChannelSlack, TargetURL: "http://x",
		}},
		{"window zero", domain.AlertRule{
			Name: "x", Threshold: 10, Channel: domain.AlertChannelSlack, TargetURL: "http://x",
		}},
		{"channel inválido", domain.AlertRule{
			Name: "x", Threshold: 10, WindowSeconds: 300, Channel: "email", TargetURL: "http://x",
		}},
		{"targetUrl vazio", domain.AlertRule{
			Name: "x", Threshold: 10, WindowSeconds: 300, Channel: domain.AlertChannelSlack,
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := svc.Create(context.Background(), c.rule)
			if err == nil {
				t.Fatal("esperava erro de validação")
			}
			var ve *domain.ValidationError
			if !errors.As(err, &ve) {
				t.Errorf("esperava ValidationError, veio %T", err)
			}
		})
	}
}

func TestAlert_CreateHappyPathComSilenceDefault(t *testing.T) {
	svc, store := newAlertSvc()
	got, err := svc.Create(context.Background(), domain.AlertRule{
		Name: "5xx-checkout", Threshold: 5, WindowSeconds: 60,
		Channel: domain.AlertChannelWebhook, TargetURL: "http://hook.local",
		// SilenceSeconds omitido → default 900
	})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if got.SilenceSeconds != 900 {
		t.Errorf("silence default esperado 900, veio %d", got.SilenceSeconds)
	}
	if len(store.rules) != 1 {
		t.Errorf("regra não persistiu")
	}
}

func TestAlert_ListRetornaTudo(t *testing.T) {
	svc, _ := newAlertSvc()
	for i := 0; i < 3; i++ {
		_, _ = svc.Create(context.Background(), domain.AlertRule{
			Name: "r", Threshold: 1, WindowSeconds: 60,
			Channel: domain.AlertChannelSlack, TargetURL: "http://x",
		})
	}
	list, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("esperava 3 regras, veio %d", len(list))
	}
}

func TestAlert_GetInexistenteRetornaNotFound(t *testing.T) {
	svc, _ := newAlertSvc()
	_, err := svc.Get(context.Background(), "no-id")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("esperava NotFound, veio %v", err)
	}
}

func TestAlert_DeleteHappyPath(t *testing.T) {
	svc, store := newAlertSvc()
	r, _ := svc.Create(context.Background(), domain.AlertRule{
		Name: "r", Threshold: 1, WindowSeconds: 60,
		Channel: domain.AlertChannelSlack, TargetURL: "http://x",
	})
	if err := svc.Delete(context.Background(), r.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(store.rules) != 0 {
		t.Errorf("regra não foi removida")
	}
}

func TestAlert_DeleteInexistenteRetornaNotFound(t *testing.T) {
	svc, _ := newAlertSvc()
	err := svc.Delete(context.Background(), "no-id")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("esperava NotFound, veio %v", err)
	}
}
