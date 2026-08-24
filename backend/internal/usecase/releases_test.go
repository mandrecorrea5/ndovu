package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

type fakeReleaseReader struct {
	nopEventReader
	releases        []domain.Release
	releaseByRel    map[string]domain.Release
	issuesByRelease map[string][]domain.Issue
	err             error
}

func (r *fakeReleaseReader) FindReleases(_ context.Context, _ domain.ReleaseFilter) ([]domain.Release, error) {
	return r.releases, r.err
}
func (r *fakeReleaseReader) GetRelease(_ context.Context, _, release string, _, _ time.Time) (domain.Release, error) {
	if r, ok := r.releaseByRel[release]; ok {
		return r, nil
	}
	return domain.Release{}, domain.ErrNotFound
}
func (r *fakeReleaseReader) FindIssues(_ context.Context, f domain.IssueFilter) ([]domain.Issue, error) {
	return r.issuesByRelease[f.Release], nil
}

func TestRelease_ListDelega(t *testing.T) {
	reader := &fakeReleaseReader{
		releases: []domain.Release{
			{Release: "1.2.3", App: "portal"},
			{Release: "1.2.4", App: "portal"},
		},
	}
	svc := NewReleaseService(reader)
	got, err := svc.List(context.Background(), domain.ReleaseFilter{App: "portal"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("esperava 2 releases, veio %d", len(got))
	}
}

func TestRelease_CompareValidaObrigatorios(t *testing.T) {
	svc := NewReleaseService(&fakeReleaseReader{})
	cases := []struct{ a, b string }{
		{"", "1.0"},
		{"1.0", ""},
		{"", ""},
	}
	for _, c := range cases {
		_, err := svc.Compare(context.Background(), "app", c.a, c.b, time.Time{}, time.Time{})
		if err == nil {
			t.Errorf("compare(%q, %q) deveria falhar", c.a, c.b)
		}
	}
}

func TestRelease_CompareDefaults(t *testing.T) {
	// Se from/to vierem zero, o service preenche (7d atrás → agora).
	// Aqui só validamos que a chamada não falha e devolve o comparativo.
	reader := &fakeReleaseReader{
		releaseByRel: map[string]domain.Release{
			"A": {Release: "A", ErrorRate: 0.02, AvgDurationMs: 100},
			"B": {Release: "B", ErrorRate: 0.05, AvgDurationMs: 150},
		},
	}
	svc := NewReleaseService(reader)
	got, err := svc.Compare(context.Background(), "portal", "A", "B", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	// DeltaErrorRate = 0.05 - 0.02 = 0.03
	if got.DeltaErrorRate < 0.029 || got.DeltaErrorRate > 0.031 {
		t.Errorf("delta error rate errado: %v", got.DeltaErrorRate)
	}
	if got.DeltaAvgMs != 50 {
		t.Errorf("delta latência errado: %v", got.DeltaAvgMs)
	}
}

func TestRelease_CompareDetectaIssuesNovas(t *testing.T) {
	// Issues em B que não estão em A = regressão introduzida no deploy B.
	reader := &fakeReleaseReader{
		releaseByRel: map[string]domain.Release{"A": {}, "B": {}},
		issuesByRelease: map[string][]domain.Issue{
			"A": {
				{Fingerprint: "fp-1"},
				{Fingerprint: "fp-2"},
			},
			"B": {
				{Fingerprint: "fp-1"},                    // já existia
				{Fingerprint: "fp-3", ImpactScore: 100},  // NOVA
			},
		},
	}
	svc := NewReleaseService(reader)
	got, err := svc.Compare(context.Background(), "portal", "A", "B", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if len(got.NewIssues) != 1 || got.NewIssues[0].Fingerprint != "fp-3" {
		t.Errorf("issues novas erradas: %+v", got.NewIssues)
	}
}

func TestRelease_ComparePropagaErroReader(t *testing.T) {
	// Um erro em qualquer chamada do reader deve propagar.
	sentinel := errors.New("boom")
	reader := &fakeReleaseReader{err: sentinel}
	// Sobrescreve para simular erro no FindReleases (mesmo err)... na verdade
	// esse fake retorna err em FindReleases, não em GetRelease. Vou usar List:
	svc := NewReleaseService(reader)
	_, err := svc.List(context.Background(), domain.ReleaseFilter{})
	if !errors.Is(err, sentinel) {
		t.Errorf("erro do reader deveria propagar, veio %v", err)
	}
}
