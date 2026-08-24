package usecase

import (
	"context"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// nopEventReader é uma implementação vazia de domain.EventReader.
// Testes embarcam anonimamente e sobrescrevem só os métodos que precisam
// — evita boilerplate quando o service testado toca em poucos métodos.
//
//   type myFakeReader struct {
//       nopEventReader
//       // campos custom
//   }
//   func (r *myFakeReader) FindIssues(...) {...}
type nopEventReader struct{}

func (nopEventReader) FindEvents(context.Context, domain.EventFilter) (domain.EventPage, error) {
	return domain.EventPage{}, nil
}
func (nopEventReader) GetEvent(context.Context, string) (domain.TraceEvent, error) {
	return domain.TraceEvent{}, nil
}
func (nopEventReader) FindSessions(context.Context, domain.SessionFilter) (domain.SessionPage, error) {
	return domain.SessionPage{}, nil
}
func (nopEventReader) GetSession(context.Context, string) (domain.Session, error) {
	return domain.Session{}, domain.ErrNotFound
}
func (nopEventReader) SessionTimeline(context.Context, string) ([]domain.TraceEvent, error) {
	return nil, nil
}
func (nopEventReader) GetOverview(context.Context, time.Time, time.Time, string) (domain.Overview, error) {
	return domain.Overview{}, nil
}
func (nopEventReader) GetFilterOptions(context.Context) (domain.FilterOptions, error) {
	return domain.FilterOptions{}, nil
}
func (nopEventReader) FindIssues(context.Context, domain.IssueFilter) ([]domain.Issue, error) {
	return nil, nil
}
func (nopEventReader) FindWebVitals(context.Context, domain.WebVitalFilter) ([]domain.WebVitalStat, error) {
	return nil, nil
}
func (nopEventReader) CountErrorsSince(context.Context, string, string, time.Time) (int, error) {
	return 0, nil
}
func (nopEventReader) MetricInWindow(context.Context, string, string, time.Time, time.Time) (float64, error) {
	return 0, nil
}
func (nopEventReader) FindReleases(context.Context, domain.ReleaseFilter) ([]domain.Release, error) {
	return nil, nil
}
func (nopEventReader) GetRelease(context.Context, string, string, time.Time, time.Time) (domain.Release, error) {
	return domain.Release{}, nil
}
func (nopEventReader) RunFunnel(context.Context, domain.FunnelRun) (domain.FunnelResult, error) {
	return domain.FunnelResult{}, nil
}
func (nopEventReader) Retention(context.Context, domain.RetentionFilter) (domain.RetentionResult, error) {
	return domain.RetentionResult{Cohorts: []domain.RetentionCohort{}}, nil
}
func (nopEventReader) TraceTimeline(context.Context, string) ([]domain.TraceEvent, error) {
	return nil, nil
}
func (nopEventReader) FindEventsByUser(context.Context, string) ([]domain.TraceEvent, error) {
	return nil, nil
}
func (nopEventReader) DeleteEventsByUser(context.Context, string) error {
	return nil
}
