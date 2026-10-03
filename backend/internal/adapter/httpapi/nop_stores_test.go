package httpapi

// Stubs mínimos ("nop") de todos os stores/ports do domínio. Cada teste
// pode instanciar direto (comportamento default = vazio/ErrNotFound) ou
// embarcar anonimamente e sobrescrever só os métodos que exercita.
//
// Sem generics ou reflection: cada nop é uma struct pequena com métodos
// que devolvem zero value / ErrNotFound. A trade-off de manutenção
// (adicionar um método na interface exige refletir aqui) é aceita por
// tornar os testes triviais de escrever.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// -----------------------------------------------------------------------
// EventStream / EventReader / EventWriter
// -----------------------------------------------------------------------

type nopStream struct{ published []domain.IngestBatch }

func (s *nopStream) Publish(_ context.Context, b domain.IngestBatch) error {
	s.published = append(s.published, b)
	return nil
}

type nopReader struct{}

func (nopReader) FindEvents(context.Context, domain.EventFilter) (domain.EventPage, error) {
	return domain.EventPage{Events: []domain.TraceEvent{}}, nil
}
func (nopReader) GetEvent(context.Context, string) (domain.TraceEvent, error) {
	return domain.TraceEvent{}, domain.ErrNotFound
}
func (nopReader) FindSessions(context.Context, domain.SessionFilter) (domain.SessionPage, error) {
	return domain.SessionPage{Sessions: []domain.Session{}}, nil
}
func (nopReader) GetSession(context.Context, string) (domain.Session, error) {
	return domain.Session{}, domain.ErrNotFound
}
func (nopReader) SessionTimeline(context.Context, string) ([]domain.TraceEvent, error) {
	return nil, nil
}
func (nopReader) GetOverview(context.Context, time.Time, time.Time, string) (domain.Overview, error) {
	return domain.Overview{}, nil
}
func (nopReader) GetFilterOptions(context.Context) (domain.FilterOptions, error) {
	return domain.FilterOptions{Apps: []string{}, Types: []string{}, Features: []string{}, Names: []string{}}, nil
}
func (nopReader) FindIssues(context.Context, domain.IssueFilter) ([]domain.Issue, error) {
	return []domain.Issue{}, nil
}
func (nopReader) FindWebVitals(context.Context, domain.WebVitalFilter) ([]domain.WebVitalStat, error) {
	return nil, nil
}
func (nopReader) CountErrorsSince(context.Context, string, string, time.Time) (int, error) {
	return 0, nil
}
func (nopReader) MetricInWindow(context.Context, string, string, time.Time, time.Time) (float64, error) {
	return 0, nil
}
func (nopReader) FindReleases(context.Context, domain.ReleaseFilter) ([]domain.Release, error) {
	return nil, nil
}
func (nopReader) GetRelease(context.Context, string, string, time.Time, time.Time) (domain.Release, error) {
	return domain.Release{}, nil
}
func (nopReader) RunFunnel(context.Context, domain.FunnelRun) (domain.FunnelResult, error) {
	return domain.FunnelResult{}, nil
}
func (nopReader) Retention(context.Context, domain.RetentionFilter) (domain.RetentionResult, error) {
	return domain.RetentionResult{Cohorts: []domain.RetentionCohort{}}, nil
}
func (nopReader) TraceTimeline(context.Context, string) ([]domain.TraceEvent, error) {
	return nil, nil
}
func (nopReader) FindEventsByUser(context.Context, string) ([]domain.TraceEvent, error) {
	return nil, nil
}
func (nopReader) DeleteEventsByUser(context.Context, string) error { return nil }

// -----------------------------------------------------------------------
// Control plane: users, companies, apps, keys
// -----------------------------------------------------------------------

type nopUserStore struct {
	users   map[string]domain.User // por id
	hashes  map[string]string
	seq     int
	adminN  int // countActiveAdmins
}

func newNopUserStore() *nopUserStore {
	return &nopUserStore{
		users: map[string]domain.User{}, hashes: map[string]string{}, adminN: 1,
	}
}

func (f *nopUserStore) CreateUser(_ context.Context, u domain.User, hash string) (domain.User, error) {
	for _, x := range f.users {
		if x.Email == u.Email {
			return domain.User{}, domain.ErrConflict
		}
	}
	f.seq++
	u.ID = "u-" + string(rune('a'+f.seq))
	u.CreatedAt = time.Now().UTC()
	u.UpdatedAt = u.CreatedAt
	f.users[u.ID] = u
	f.hashes[u.ID] = hash
	if u.Role == domain.RoleAdmin && u.Active {
		f.adminN++
	}
	return u, nil
}
func (f *nopUserStore) GetUserByEmail(_ context.Context, email string) (domain.User, string, error) {
	for id, u := range f.users {
		if u.Email == email {
			return u, f.hashes[id], nil
		}
	}
	return domain.User{}, "", domain.ErrNotFound
}
func (f *nopUserStore) GetUserByID(_ context.Context, id string) (domain.User, error) {
	if u, ok := f.users[id]; ok {
		return u, nil
	}
	return domain.User{}, domain.ErrNotFound
}
func (f *nopUserStore) ListUsers(_ context.Context) ([]domain.User, error) {
	out := []domain.User{}
	for _, u := range f.users {
		out = append(out, u)
	}
	return out, nil
}
func (f *nopUserStore) ListUsersByCompany(_ context.Context, companyID string) ([]domain.User, error) {
	out := []domain.User{}
	for _, u := range f.users {
		if u.CompanyID == companyID {
			out = append(out, u)
		}
	}
	return out, nil
}
func (f *nopUserStore) UpdateUser(_ context.Context, id string, role *domain.Role, active *bool, name *string, companyID *string) (domain.User, error) {
	u, ok := f.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	if role != nil {
		u.Role = *role
	}
	if active != nil {
		u.Active = *active
	}
	if name != nil {
		u.Name = *name
	}
	if companyID != nil {
		u.CompanyID = *companyID
	}
	u.UpdatedAt = time.Now().UTC()
	f.users[id] = u
	return u, nil
}
func (f *nopUserStore) SetPassword(_ context.Context, id, hash string) error {
	if _, ok := f.users[id]; !ok {
		return domain.ErrNotFound
	}
	f.hashes[id] = hash
	return nil
}
func (f *nopUserStore) SetSuperAdmin(_ context.Context, id string) error {
	u, ok := f.users[id]
	if !ok || u.Role != domain.RoleAdmin {
		return domain.ErrNotFound
	}
	u.IsSuper = true
	f.users[id] = u
	return nil
}
func (f *nopUserStore) CountActiveAdmins(context.Context) (int, error) { return f.adminN, nil }

type nopCompanyStore struct {
	items map[string]domain.Company
	seq   int
}

func newNopCompanyStore() *nopCompanyStore {
	return &nopCompanyStore{
		items: map[string]domain.Company{
			"c-default": {ID: "c-default", Name: "Padrão", Active: true},
		},
	}
}

func (f *nopCompanyStore) CreateCompany(_ context.Context, c domain.Company) (domain.Company, error) {
	f.seq++
	c.ID = "c-" + string(rune('a'+f.seq))
	f.items[c.ID] = c
	return c, nil
}
func (f *nopCompanyStore) GetCompany(_ context.Context, id string) (domain.Company, error) {
	if c, ok := f.items[id]; ok {
		return c, nil
	}
	return domain.Company{}, domain.ErrNotFound
}
func (f *nopCompanyStore) ListCompanies(context.Context) ([]domain.Company, error) {
	out := []domain.Company{}
	for _, c := range f.items {
		out = append(out, c)
	}
	return out, nil
}
func (f *nopCompanyStore) UpdateCompany(_ context.Context, id string, c domain.Company) (domain.Company, error) {
	if _, ok := f.items[id]; !ok {
		return domain.Company{}, domain.ErrNotFound
	}
	c.ID = id
	f.items[id] = c
	return c, nil
}
func (f *nopCompanyStore) DeleteCompany(_ context.Context, id string) error {
	if _, ok := f.items[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.items, id)
	return nil
}

type nopAppStore struct {
	items map[string]domain.App
	seq   int
}

func newNopAppStore() *nopAppStore {
	return &nopAppStore{items: map[string]domain.App{}}
}

func (f *nopAppStore) CreateApp(_ context.Context, a domain.App) (domain.App, error) {
	f.seq++
	a.ID = "app-" + string(rune('a'+f.seq))
	a.CreatedAt = time.Now().UTC()
	a.UpdatedAt = a.CreatedAt
	f.items[a.ID] = a
	return a, nil
}
func (f *nopAppStore) GetApp(_ context.Context, id string) (domain.App, error) {
	if a, ok := f.items[id]; ok {
		return a, nil
	}
	return domain.App{}, domain.ErrNotFound
}
func (f *nopAppStore) GetAppByName(_ context.Context, name string) (domain.App, error) {
	for _, a := range f.items {
		if a.Name == name {
			return a, nil
		}
	}
	return domain.App{}, domain.ErrNotFound
}
func (f *nopAppStore) ListApps(context.Context) ([]domain.App, error) {
	out := []domain.App{}
	for _, a := range f.items {
		out = append(out, a)
	}
	return out, nil
}
func (f *nopAppStore) ListAppsByCompany(_ context.Context, companyID string) ([]domain.App, error) {
	out := []domain.App{}
	for _, a := range f.items {
		if a.CompanyID == companyID {
			out = append(out, a)
		}
	}
	return out, nil
}
func (f *nopAppStore) UpdateApp(_ context.Context, id string, a domain.App) (domain.App, error) {
	if _, ok := f.items[id]; !ok {
		return domain.App{}, domain.ErrNotFound
	}
	a.ID = id
	f.items[id] = a
	return a, nil
}
func (f *nopAppStore) DeleteApp(_ context.Context, id string) error {
	if _, ok := f.items[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.items, id)
	return nil
}
func (f *nopAppStore) ListAppNamesByCompany(_ context.Context, companyID string) ([]string, error) {
	out := []string{}
	for _, a := range f.items {
		if a.CompanyID == companyID {
			out = append(out, a.Name)
		}
	}
	return out, nil
}

type nopKeyStore struct {
	keys   map[string]domain.APIKey
	hashes map[string]string
	seq    int
}

func newNopKeyStore() *nopKeyStore {
	return &nopKeyStore{keys: map[string]domain.APIKey{}, hashes: map[string]string{}}
}

func (f *nopKeyStore) CreateAPIKey(_ context.Context, k domain.APIKey, hash, _ string) (domain.APIKey, error) {
	for id, old := range f.keys {
		if k.AppID != "" && old.AppID == k.AppID && old.Active {
			old.Active = false
			revokedAt := time.Now().UTC()
			old.RevokedAt = &revokedAt
			f.keys[id] = old
		}
	}
	f.seq++
	k.ID = "k-" + string(rune('a'+f.seq))
	k.Active = true
	k.CreatedAt = time.Now().UTC()
	f.keys[k.ID] = k
	f.hashes[k.ID] = hash
	return k, nil
}
func (f *nopKeyStore) ListAPIKeys(context.Context) ([]domain.APIKey, error) {
	out := []domain.APIKey{}
	for _, k := range f.keys {
		out = append(out, k)
	}
	return out, nil
}
func (f *nopKeyStore) ListAPIKeysByCompany(context.Context, string) ([]domain.APIKey, error) {
	return f.ListAPIKeys(context.Background())
}
func (f *nopKeyStore) RevokeAPIKey(_ context.Context, id string) error {
	k, ok := f.keys[id]
	if !ok {
		return domain.ErrNotFound
	}
	k.Active = false
	f.keys[id] = k
	return nil
}
func (f *nopKeyStore) FindActiveKeyByHash(_ context.Context, hash string) (domain.APIKey, error) {
	for id, h := range f.hashes {
		if h == hash && f.keys[id].Active {
			return f.keys[id], nil
		}
	}
	return domain.APIKey{}, domain.ErrNotFound
}
func (f *nopKeyStore) GetAPIKeyByID(_ context.Context, id string) (domain.APIKey, error) {
	if k, ok := f.keys[id]; ok {
		return k, nil
	}
	return domain.APIKey{}, domain.ErrNotFound
}

// -----------------------------------------------------------------------
// Stores estendidos
// -----------------------------------------------------------------------

type nopIssueStore struct {
	states   map[string]domain.IssueState
	comments []domain.IssueComment
	assignee map[string][]string
}

func newNopIssueStore() *nopIssueStore {
	return &nopIssueStore{states: map[string]domain.IssueState{}, assignee: map[string][]string{}}
}

func (f *nopIssueStore) GetIssueStates(_ context.Context, fps []string) (map[string]domain.IssueState, error) {
	out := map[string]domain.IssueState{}
	for _, fp := range fps {
		if s, ok := f.states[fp]; ok {
			out[fp] = s
		}
	}
	return out, nil
}
func (f *nopIssueStore) UpsertIssueStatus(_ context.Context, in domain.IssueStatusInput) error {
	f.states[in.Fingerprint] = domain.IssueState{Status: in.Status, AssigneeUserID: in.AssigneeUserID}
	return nil
}
func (f *nopIssueStore) ListIssueComments(_ context.Context, fp string) ([]domain.IssueComment, error) {
	out := []domain.IssueComment{}
	for _, c := range f.comments {
		if c.Fingerprint == fp {
			out = append(out, c)
		}
	}
	return out, nil
}
func (f *nopIssueStore) CreateIssueComment(_ context.Context, fp, authorID, body string) (domain.IssueComment, error) {
	c := domain.IssueComment{
		ID:          "c-" + string(rune('a'+len(f.comments))),
		Fingerprint: fp, AuthorID: authorID, Body: body, CreatedAt: time.Now().UTC(),
	}
	f.comments = append(f.comments, c)
	return c, nil
}
func (f *nopIssueStore) DeleteIssueComment(_ context.Context, id, requesterID string) error {
	for i, c := range f.comments {
		if c.ID == id {
			if c.AuthorID != requesterID {
				return domain.ErrForbidden
			}
			f.comments = append(f.comments[:i], f.comments[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}
func (f *nopIssueStore) ListFingerprintsByAssignee(_ context.Context, userID string) ([]string, error) {
	return f.assignee[userID], nil
}

type nopFeedbackStore struct {
	items map[string]domain.UserFeedback
	seq   int
}

func newNopFeedbackStore() *nopFeedbackStore {
	return &nopFeedbackStore{items: map[string]domain.UserFeedback{}}
}

func (f *nopFeedbackStore) CreateFeedback(_ context.Context, fb domain.UserFeedback) (domain.UserFeedback, error) {
	f.seq++
	fb.ID = "fb-" + string(rune('a'+f.seq))
	fb.Status = domain.FeedbackNew
	fb.CreatedAt = time.Now().UTC()
	f.items[fb.ID] = fb
	return fb, nil
}
func (f *nopFeedbackStore) GetFeedback(_ context.Context, id string) (domain.UserFeedback, error) {
	if fb, ok := f.items[id]; ok {
		return fb, nil
	}
	return domain.UserFeedback{}, domain.ErrNotFound
}
func (f *nopFeedbackStore) UpdateFeedbackStatus(_ context.Context, id string, status domain.FeedbackStatus, by string) (domain.UserFeedback, error) {
	fb, ok := f.items[id]
	if !ok {
		return domain.UserFeedback{}, domain.ErrNotFound
	}
	fb.Status = status
	fb.ResolvedBy = by
	f.items[id] = fb
	return fb, nil
}
func (f *nopFeedbackStore) ListFeedbacks(_ context.Context, filter domain.FeedbackFilter) ([]domain.UserFeedback, int, error) {
	out := []domain.UserFeedback{}
	for _, fb := range f.items {
		if filter.App != "" && fb.App != filter.App {
			continue
		}
		if filter.SessionID != "" && fb.SessionID != filter.SessionID {
			continue
		}
		out = append(out, fb)
	}
	return out, len(out), nil
}
func (f *nopFeedbackStore) DeleteFeedback(_ context.Context, id string) error {
	if _, ok := f.items[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.items, id)
	return nil
}

type nopAlertStore struct {
	items map[string]domain.AlertRule
	seq   int
}

func newNopAlertStore() *nopAlertStore {
	return &nopAlertStore{items: map[string]domain.AlertRule{}}
}

func (f *nopAlertStore) CreateAlertRule(_ context.Context, r domain.AlertRule) (domain.AlertRule, error) {
	f.seq++
	r.ID = "al-" + string(rune('a'+f.seq))
	f.items[r.ID] = r
	return r, nil
}
func (f *nopAlertStore) GetAlertRule(_ context.Context, id string) (domain.AlertRule, error) {
	if r, ok := f.items[id]; ok {
		return r, nil
	}
	return domain.AlertRule{}, domain.ErrNotFound
}
func (f *nopAlertStore) ListAlertRules(context.Context) ([]domain.AlertRule, error) {
	out := []domain.AlertRule{}
	for _, r := range f.items {
		out = append(out, r)
	}
	return out, nil
}
func (f *nopAlertStore) DeleteAlertRule(_ context.Context, id string) error {
	if _, ok := f.items[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.items, id)
	return nil
}
func (f *nopAlertStore) LastDeliveryAt(context.Context, string) (time.Time, error) {
	return time.Time{}, nil
}
func (f *nopAlertStore) RecordDelivery(context.Context, string, int, bool, string) error {
	return nil
}

type nopAnomalyStore struct {
	rules      map[string]domain.AnomalyRule
	detections []domain.AnomalyDetection
	seq        int
}

func newNopAnomalyStore() *nopAnomalyStore {
	return &nopAnomalyStore{rules: map[string]domain.AnomalyRule{}}
}
func (f *nopAnomalyStore) CreateAnomalyRule(_ context.Context, r domain.AnomalyRule) (domain.AnomalyRule, error) {
	f.seq++
	r.ID = "an-" + string(rune('a'+f.seq))
	f.rules[r.ID] = r
	return r, nil
}
func (f *nopAnomalyStore) GetAnomalyRule(_ context.Context, id string) (domain.AnomalyRule, error) {
	if r, ok := f.rules[id]; ok {
		return r, nil
	}
	return domain.AnomalyRule{}, domain.ErrNotFound
}
func (f *nopAnomalyStore) UpdateAnomalyRule(_ context.Context, id string, r domain.AnomalyRule) (domain.AnomalyRule, error) {
	if _, ok := f.rules[id]; !ok {
		return domain.AnomalyRule{}, domain.ErrNotFound
	}
	r.ID = id
	f.rules[id] = r
	return r, nil
}
func (f *nopAnomalyStore) DeleteAnomalyRule(_ context.Context, id string) error {
	if _, ok := f.rules[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.rules, id)
	return nil
}
func (f *nopAnomalyStore) ListAnomalyRules(context.Context) ([]domain.AnomalyRule, error) {
	out := []domain.AnomalyRule{}
	for _, r := range f.rules {
		out = append(out, r)
	}
	return out, nil
}
func (f *nopAnomalyStore) LastDetection(context.Context, string) (domain.AnomalyDetection, error) {
	return domain.AnomalyDetection{}, domain.ErrNotFound
}
func (f *nopAnomalyStore) RecordDetection(_ context.Context, d domain.AnomalyDetection) error {
	f.detections = append(f.detections, d)
	return nil
}
func (f *nopAnomalyStore) ListDetections(context.Context, int, int) ([]domain.AnomalyDetection, int, error) {
	return f.detections, len(f.detections), nil
}

type nopSamplingStore struct {
	items map[string]domain.SamplingRule
	seq   int
}

func newNopSamplingStore() *nopSamplingStore {
	return &nopSamplingStore{items: map[string]domain.SamplingRule{}}
}

func (f *nopSamplingStore) CreateSamplingRule(_ context.Context, r domain.SamplingRule) (domain.SamplingRule, error) {
	f.seq++
	r.ID = "sa-" + string(rune('a'+f.seq))
	f.items[r.ID] = r
	return r, nil
}
func (f *nopSamplingStore) GetSamplingRule(_ context.Context, id string) (domain.SamplingRule, error) {
	if r, ok := f.items[id]; ok {
		return r, nil
	}
	return domain.SamplingRule{}, domain.ErrNotFound
}
func (f *nopSamplingStore) UpdateSamplingRule(_ context.Context, id string, r domain.SamplingRule) (domain.SamplingRule, error) {
	if _, ok := f.items[id]; !ok {
		return domain.SamplingRule{}, domain.ErrNotFound
	}
	r.ID = id
	f.items[id] = r
	return r, nil
}
func (f *nopSamplingStore) DeleteSamplingRule(_ context.Context, id string) error {
	if _, ok := f.items[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.items, id)
	return nil
}
func (f *nopSamplingStore) ListSamplingRules(context.Context) ([]domain.SamplingRule, error) {
	out := []domain.SamplingRule{}
	for _, r := range f.items {
		out = append(out, r)
	}
	return out, nil
}

type nopSourceMapStore struct {
	items map[string]domain.SourceMap
	seq   int
}

func newNopSourceMapStore() *nopSourceMapStore {
	return &nopSourceMapStore{items: map[string]domain.SourceMap{}}
}

func (f *nopSourceMapStore) UpsertSourceMap(_ context.Context, m domain.SourceMap, _ string) (domain.SourceMap, error) {
	f.seq++
	m.ID = "sm-" + string(rune('a'+f.seq))
	f.items[m.ID] = m
	return m, nil
}
func (f *nopSourceMapStore) GetSourceMap(_ context.Context, id string) (domain.SourceMap, error) {
	if m, ok := f.items[id]; ok {
		return m, nil
	}
	return domain.SourceMap{}, domain.ErrNotFound
}
func (f *nopSourceMapStore) ListSourceMaps(_ context.Context, app, release string) ([]domain.SourceMap, error) {
	out := []domain.SourceMap{}
	for _, m := range f.items {
		if app != "" && m.App != app {
			continue
		}
		if release != "" && m.Release != release {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}
func (f *nopSourceMapStore) GetSourceMapContent(context.Context, string, string, string) (string, error) {
	return "", nil
}
func (f *nopSourceMapStore) DeleteSourceMap(_ context.Context, id string) error {
	if _, ok := f.items[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.items, id)
	return nil
}

type nopSnapshotMetaStore struct{ items map[string]domain.SessionSnapshot }

func (f *nopSnapshotMetaStore) CreateSnapshotMeta(_ context.Context, s domain.SessionSnapshot) error {
	if f.items == nil {
		f.items = map[string]domain.SessionSnapshot{}
	}
	f.items[s.EventID] = s
	return nil
}
func (f *nopSnapshotMetaStore) GetSnapshotMetaByEvent(_ context.Context, id string) (domain.SessionSnapshot, error) {
	if s, ok := f.items[id]; ok {
		return s, nil
	}
	return domain.SessionSnapshot{}, domain.ErrNotFound
}

type nopSnapshotBlobStore struct{ items map[string][]byte }

func (f *nopSnapshotBlobStore) PutSnapshot(_ context.Context, key string, content []byte) error {
	if f.items == nil {
		f.items = map[string][]byte{}
	}
	f.items[key] = content
	return nil
}
func (f *nopSnapshotBlobStore) GetSnapshot(_ context.Context, key string) ([]byte, error) {
	if b, ok := f.items[key]; ok {
		return b, nil
	}
	return nil, domain.ErrNotFound
}

type nopPermStore struct{ grants []domain.UserAppPermission }

func (f *nopPermStore) GrantAppPermission(_ context.Context, userID, appID, role, _ string) (domain.UserAppPermission, error) {
	for i, g := range f.grants {
		if g.UserID == userID && g.AppID == appID {
			f.grants[i].Role = role
			return f.grants[i], nil
		}
	}
	p := domain.UserAppPermission{UserID: userID, AppID: appID, Role: role, GrantedAt: time.Now().UTC()}
	f.grants = append(f.grants, p)
	return p, nil
}
func (f *nopPermStore) RevokeAppPermission(_ context.Context, userID, appID string) error {
	for i, g := range f.grants {
		if g.UserID == userID && g.AppID == appID {
			f.grants = append(f.grants[:i], f.grants[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}
func (f *nopPermStore) ListAppPermissionsForUser(_ context.Context, userID string) ([]domain.UserAppPermission, error) {
	out := []domain.UserAppPermission{}
	for _, g := range f.grants {
		if g.UserID == userID {
			out = append(out, g)
		}
	}
	return out, nil
}
func (f *nopPermStore) ListAppNamesForUser(context.Context, string) ([]string, error) {
	return nil, nil
}

type nopSavedViewStore struct {
	items map[string]domain.SavedView
	seq   int
}

func newNopSavedViewStore() *nopSavedViewStore {
	return &nopSavedViewStore{items: map[string]domain.SavedView{}}
}
func (f *nopSavedViewStore) CreateSavedView(_ context.Context, v domain.SavedView) (domain.SavedView, error) {
	f.seq++
	v.ID = "sv-" + string(rune('a'+f.seq))
	f.items[v.ID] = v
	return v, nil
}
func (f *nopSavedViewStore) UpdateSavedView(_ context.Context, id, owner string, name string, filters json.RawMessage, isShared bool) (domain.SavedView, error) {
	v, ok := f.items[id]
	if !ok || v.OwnerUserID != owner {
		return domain.SavedView{}, domain.ErrNotFound
	}
	v.Name = name
	v.Filters = filters
	v.IsShared = isShared
	f.items[id] = v
	return v, nil
}
func (f *nopSavedViewStore) DeleteSavedView(_ context.Context, id, owner string) error {
	v, ok := f.items[id]
	if !ok || v.OwnerUserID != owner {
		return domain.ErrNotFound
	}
	delete(f.items, id)
	return nil
}
func (f *nopSavedViewStore) ListSavedViews(_ context.Context, owner, viewType string) ([]domain.SavedView, error) {
	out := []domain.SavedView{}
	for _, v := range f.items {
		if v.ViewType != viewType && viewType != "" {
			continue
		}
		if v.OwnerUserID == owner || v.IsShared {
			out = append(out, v)
		}
	}
	return out, nil
}

type nopFunnelStore struct {
	items map[string]domain.Funnel
	seq   int
}

func newNopFunnelStore() *nopFunnelStore {
	return &nopFunnelStore{items: map[string]domain.Funnel{}}
}
func (f *nopFunnelStore) CreateFunnel(_ context.Context, fn domain.Funnel) (domain.Funnel, error) {
	f.seq++
	fn.ID = "fn-" + string(rune('a'+f.seq))
	f.items[fn.ID] = fn
	return fn, nil
}
func (f *nopFunnelStore) UpdateFunnel(_ context.Context, id, name string, ws int, steps json.RawMessage) (domain.Funnel, error) {
	fn, ok := f.items[id]
	if !ok {
		return domain.Funnel{}, domain.ErrNotFound
	}
	fn.Name = name
	fn.WindowSeconds = ws
	fn.Steps = steps
	f.items[id] = fn
	return fn, nil
}
func (f *nopFunnelStore) ListFunnels(_ context.Context, app string) ([]domain.Funnel, error) {
	out := []domain.Funnel{}
	for _, fn := range f.items {
		if app == "" || fn.App == app {
			out = append(out, fn)
		}
	}
	return out, nil
}
func (f *nopFunnelStore) GetFunnel(_ context.Context, id string) (domain.Funnel, error) {
	if fn, ok := f.items[id]; ok {
		return fn, nil
	}
	return domain.Funnel{}, domain.ErrNotFound
}
func (f *nopFunnelStore) DeleteFunnel(_ context.Context, id string) error {
	if _, ok := f.items[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.items, id)
	return nil
}

type nopAuditStore struct{ entries []domain.AuditEntry }

func (f *nopAuditStore) RecordAudit(_ context.Context, e domain.AuditEntry) error {
	f.entries = append(f.entries, e)
	return nil
}
func (f *nopAuditStore) ListAudit(_ context.Context, _ domain.AuditFilter) ([]domain.AuditEntry, int, error) {
	return f.entries, len(f.entries), nil
}
