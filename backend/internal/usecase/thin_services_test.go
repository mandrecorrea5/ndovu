package usecase

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// Cobertura complementar dos services "thin" que delegam quase tudo ao
// store — cada um tem 2-5 métodos e vale ~3 testes rápidos.

// -----------------------------------------------------------------------
// CompanyService
// -----------------------------------------------------------------------

// fakeCompanyStoreFull cobre todos os métodos da interface.
type fakeCompanyStoreFull struct {
	items map[string]domain.Company
	seq   int
	failOnDelete error
}

func newFakeCompanyStoreFull() *fakeCompanyStoreFull {
	return &fakeCompanyStoreFull{items: map[string]domain.Company{}}
}

func (f *fakeCompanyStoreFull) CreateCompany(_ context.Context, c domain.Company) (domain.Company, error) {
	for _, existing := range f.items {
		if strings.EqualFold(existing.Name, c.Name) {
			return domain.Company{}, domain.ErrConflict
		}
	}
	f.seq++
	c.ID = string(rune('a' + f.seq))
	f.items[c.ID] = c
	return c, nil
}
func (f *fakeCompanyStoreFull) GetCompany(_ context.Context, id string) (domain.Company, error) {
	if c, ok := f.items[id]; ok {
		return c, nil
	}
	return domain.Company{}, domain.ErrNotFound
}
func (f *fakeCompanyStoreFull) ListCompanies(_ context.Context) ([]domain.Company, error) {
	out := []domain.Company{}
	for _, c := range f.items {
		out = append(out, c)
	}
	return out, nil
}
func (f *fakeCompanyStoreFull) UpdateCompany(_ context.Context, id string, c domain.Company) (domain.Company, error) {
	if _, ok := f.items[id]; !ok {
		return domain.Company{}, domain.ErrNotFound
	}
	c.ID = id
	f.items[id] = c
	return c, nil
}
func (f *fakeCompanyStoreFull) DeleteCompany(_ context.Context, id string) error {
	if f.failOnDelete != nil {
		return f.failOnDelete
	}
	if _, ok := f.items[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.items, id)
	return nil
}

func newCompanySvc() (*CompanyService, *fakeCompanyStoreFull) {
	store := newFakeCompanyStoreFull()
	return NewCompanyService(store, slog.New(slog.NewTextHandler(io.Discard, nil))), store
}

func TestCompany_CreateValidaNome(t *testing.T) {
	svc, _ := newCompanySvc()
	_, err := svc.Create(context.Background(), CreateCompanyInput{Name: "   "})
	if err == nil {
		t.Fatal("esperava erro em name vazio")
	}
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("esperava ValidationError, veio %T", err)
	}
}

func TestCompany_CreateTrimNomeEDocumento(t *testing.T) {
	svc, store := newCompanySvc()
	c, err := svc.Create(context.Background(), CreateCompanyInput{
		Name: "  Acme  ", Document: "  12.345.678/0001-00  ", Active: true,
	})
	if err != nil {
		t.Fatalf("criar falhou: %v", err)
	}
	if c.Name != "Acme" || store.items[c.ID].Document != "12.345.678/0001-00" {
		t.Errorf("trim não aplicado: %+v", store.items[c.ID])
	}
}

func TestCompany_UpdateHappyPath(t *testing.T) {
	svc, store := newCompanySvc()
	c, _ := svc.Create(context.Background(), CreateCompanyInput{Name: "old", Active: true})
	newName := "new"
	inactive := false
	_, err := svc.Update(context.Background(), c.ID, UpdateCompanyInput{
		Name: &newName, Active: &inactive,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if store.items[c.ID].Name != "new" || store.items[c.ID].Active {
		t.Errorf("update não persistiu: %+v", store.items[c.ID])
	}
}

func TestCompany_UpdateNameVazioRejeita(t *testing.T) {
	svc, _ := newCompanySvc()
	c, _ := svc.Create(context.Background(), CreateCompanyInput{Name: "x"})
	empty := "   "
	_, err := svc.Update(context.Background(), c.ID, UpdateCompanyInput{Name: &empty})
	if err == nil {
		t.Fatal("update com name vazio deveria falhar")
	}
}

func TestCompany_UpdateCompanyInexistenteNotFound(t *testing.T) {
	svc, _ := newCompanySvc()
	_, err := svc.Update(context.Background(), "no-comp", UpdateCompanyInput{})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("esperava NotFound, veio %v", err)
	}
}

func TestCompany_ListEDeleteDelegam(t *testing.T) {
	svc, _ := newCompanySvc()
	c, _ := svc.Create(context.Background(), CreateCompanyInput{Name: "x"})
	list, _ := svc.List(context.Background())
	if len(list) != 1 {
		t.Errorf("esperava 1, veio %d", len(list))
	}
	if err := svc.Delete(context.Background(), c.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

// -----------------------------------------------------------------------
// AuditService
// -----------------------------------------------------------------------

type fakeAuditStore struct {
	entries      []domain.AuditEntry
	recordFails  bool
}

func (f *fakeAuditStore) RecordAudit(_ context.Context, e domain.AuditEntry) error {
	if f.recordFails {
		return errors.New("banco fora")
	}
	f.entries = append(f.entries, e)
	return nil
}
func (f *fakeAuditStore) ListAudit(_ context.Context, _ domain.AuditFilter) ([]domain.AuditEntry, int, error) {
	return f.entries, len(f.entries), nil
}

func TestAudit_RecordSerializaDetails(t *testing.T) {
	store := &fakeAuditStore{}
	svc := NewAuditService(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.Record(context.Background(), RecordInput{
		Actor:   domain.Identity{UserID: "u-1", Email: "u@x"},
		Action:  "user.create",
		Details: map[string]any{"role": "editor", "n": 42},
		IP:      "127.0.0.1",
	})
	if len(store.entries) != 1 {
		t.Fatalf("esperava 1 entry, veio %d", len(store.entries))
	}
	e := store.entries[0]
	if e.ActorUserID != "u-1" || e.Action != "user.create" {
		t.Errorf("dados errados: %+v", e)
	}
	if !bytes.Contains(e.Details, []byte("editor")) {
		t.Errorf("details JSON não contém 'editor': %s", e.Details)
	}
}

func TestAudit_RecordNaoPropagaErro(t *testing.T) {
	// Se o store falhar, Record só loga warning e retorna sem panic.
	store := &fakeAuditStore{recordFails: true}
	svc := NewAuditService(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Não deve panic:
	svc.Record(context.Background(), RecordInput{Action: "x"})
}

func TestAudit_RecordDetailsNil(t *testing.T) {
	store := &fakeAuditStore{}
	svc := NewAuditService(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.Record(context.Background(), RecordInput{Action: "y"})
	if len(store.entries) != 1 {
		t.Fatal("esperava 1 entry")
	}
	if store.entries[0].Details != nil {
		t.Errorf("details deveria ser nil, veio %s", store.entries[0].Details)
	}
}

func TestAudit_ListDelega(t *testing.T) {
	store := &fakeAuditStore{
		entries: []domain.AuditEntry{{ID: "e1"}, {ID: "e2"}},
	}
	svc := NewAuditService(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	list, total, err := svc.List(context.Background(), domain.AuditFilter{})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Errorf("esperava 2, veio %d/%d", len(list), total)
	}
}

// -----------------------------------------------------------------------
// GDPRService
// -----------------------------------------------------------------------

type fakeGDPRReader struct {
	nopEventReader
	deleted []string
	events  []domain.TraceEvent
	err     error
}

func (r *fakeGDPRReader) FindEventsByUser(_ context.Context, userID string) ([]domain.TraceEvent, error) {
	return r.events, r.err
}
func (r *fakeGDPRReader) DeleteEventsByUser(_ context.Context, userID string) error {
	if r.err != nil {
		return r.err
	}
	r.deleted = append(r.deleted, userID)
	return nil
}

func TestGDPR_ExportValidaUserID(t *testing.T) {
	svc := NewGDPRService(&fakeGDPRReader{}, slog.Default())
	cases := []string{"", "   "}
	for _, uid := range cases {
		_, err := svc.Export(context.Background(), uid)
		if err == nil {
			t.Errorf("userId=%q deveria falhar", uid)
		}
	}
}

func TestGDPR_ExportOK(t *testing.T) {
	reader := &fakeGDPRReader{
		events: []domain.TraceEvent{
			{ID: "e1", UserID: "u-x"},
			{ID: "e2", UserID: "u-x"},
		},
	}
	svc := NewGDPRService(reader, slog.Default())
	got, err := svc.Export(context.Background(), "u-x")
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("esperava 2 eventos, veio %d", len(got))
	}
}

func TestGDPR_ForgetValidaUserID(t *testing.T) {
	svc := NewGDPRService(&fakeGDPRReader{}, slog.Default())
	if err := svc.Forget(context.Background(), "  "); err == nil {
		t.Fatal("userId vazio deveria falhar")
	}
}

func TestGDPR_ForgetDispatchOK(t *testing.T) {
	reader := &fakeGDPRReader{}
	svc := NewGDPRService(reader, slog.Default())
	if err := svc.Forget(context.Background(), "u-x"); err != nil {
		t.Fatalf("erro: %v", err)
	}
	if len(reader.deleted) != 1 || reader.deleted[0] != "u-x" {
		t.Errorf("delete não foi chamado: %v", reader.deleted)
	}
}

// -----------------------------------------------------------------------
// SnapshotService
// -----------------------------------------------------------------------

type fakeSnapshotMeta struct {
	items map[string]domain.SessionSnapshot // eventID -> meta
}

func (f *fakeSnapshotMeta) CreateSnapshotMeta(_ context.Context, s domain.SessionSnapshot) error {
	if f.items == nil {
		f.items = map[string]domain.SessionSnapshot{}
	}
	f.items[s.EventID] = s
	return nil
}
func (f *fakeSnapshotMeta) GetSnapshotMetaByEvent(_ context.Context, eventID string) (domain.SessionSnapshot, error) {
	if s, ok := f.items[eventID]; ok {
		return s, nil
	}
	return domain.SessionSnapshot{}, domain.ErrNotFound
}

type fakeSnapshotBlob struct {
	items map[string][]byte
}

func (f *fakeSnapshotBlob) PutSnapshot(_ context.Context, key string, content []byte) error {
	if f.items == nil {
		f.items = map[string][]byte{}
	}
	f.items[key] = content
	return nil
}
func (f *fakeSnapshotBlob) GetSnapshot(_ context.Context, key string) ([]byte, error) {
	if b, ok := f.items[key]; ok {
		return b, nil
	}
	return nil, errors.New("blob not found")
}

func newSnapshotSvc() (*SnapshotService, *fakeSnapshotMeta, *fakeSnapshotBlob) {
	meta := &fakeSnapshotMeta{}
	blob := &fakeSnapshotBlob{}
	return NewSnapshotService(meta, blob, slog.Default()), meta, blob
}

func TestSnapshot_EnabledFalseSemDeps(t *testing.T) {
	svc := NewSnapshotService(nil, nil, slog.Default())
	if svc.Enabled() {
		t.Error("service sem deps deveria estar desabilitado")
	}
	_, err := svc.Save(context.Background(), SnapshotInput{})
	if err == nil {
		t.Error("Save desabilitado deveria falhar")
	}
	_, _, err = svc.Get(context.Background(), "x")
	if err == nil {
		t.Error("Get desabilitado deveria falhar")
	}
}

func TestSnapshot_SaveValidaObrigatorios(t *testing.T) {
	svc, _, _ := newSnapshotSvc()
	cases := []SnapshotInput{
		{},                                                            // tudo vazio
		{EventID: "e", SessionID: "s"},                                // sem app
		{EventID: "e", SessionID: "s", App: "a"},                      // sem html
		{EventID: "e", SessionID: "s", App: "a", HTML: strings.Repeat("x", 21*1024*1024)}, // gigante
	}
	for i, in := range cases {
		_, err := svc.Save(context.Background(), in)
		if err == nil {
			t.Errorf("case %d: deveria falhar validação", i)
		}
	}
}

func TestSnapshot_SaveGzipRoundTrip(t *testing.T) {
	svc, meta, blob := newSnapshotSvc()
	html := "<html><body>" + strings.Repeat("olá mundo ", 500) + "</body></html>"
	saved, err := svc.Save(context.Background(), SnapshotInput{
		EventID: "e-1", SessionID: "s-1", App: "portal",
		HTML: html, TakenAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("save falhou: %v", err)
	}
	// Blob deve estar gzipped — descomprime pra conferir.
	raw := blob.items[saved.ObjectKey]
	if len(raw) == 0 {
		t.Fatal("blob vazio")
	}
	r, _ := gzip.NewReader(bytes.NewReader(raw))
	got, _ := io.ReadAll(r)
	if string(got) != html {
		t.Errorf("gzip round trip falhou: %d bytes originais vs %d obtidos", len(html), len(got))
	}
	// Meta persistida.
	if _, ok := meta.items["e-1"]; !ok {
		t.Error("meta não persistiu")
	}
	// SizeBytes reflete gzip.
	if saved.SizeBytes != len(raw) {
		t.Errorf("sizeBytes divergente: %d vs %d", saved.SizeBytes, len(raw))
	}
}

func TestSnapshot_GetHappyPath(t *testing.T) {
	svc, _, _ := newSnapshotSvc()
	html := "<h1>x</h1>"
	_, err := svc.Save(context.Background(), SnapshotInput{
		EventID: "e-2", SessionID: "s", App: "a", HTML: html, TakenAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	_, gotHTML, err := svc.Get(context.Background(), "e-2")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(gotHTML) != html {
		t.Errorf("html round trip falhou: %q vs %q", gotHTML, html)
	}
}

func TestSnapshot_GetEventInexistenteRetornaNotFound(t *testing.T) {
	svc, _, _ := newSnapshotSvc()
	_, _, err := svc.Get(context.Background(), "e-nao-existe")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("esperava NotFound, veio %v", err)
	}
}

// -----------------------------------------------------------------------
// SourceMapService (só Get + Delete + List — Upload+Resolve são + complexos)
// -----------------------------------------------------------------------

type fakeSourceMapStore struct {
	items map[string]domain.SourceMap
	seq   int
}

func newFakeSourceMapStore() *fakeSourceMapStore {
	return &fakeSourceMapStore{items: map[string]domain.SourceMap{}}
}

func (f *fakeSourceMapStore) UpsertSourceMap(_ context.Context, m domain.SourceMap, _ string) (domain.SourceMap, error) {
	f.seq++
	m.ID = string(rune('a' + f.seq))
	f.items[m.ID] = m
	return m, nil
}
func (f *fakeSourceMapStore) GetSourceMap(_ context.Context, id string) (domain.SourceMap, error) {
	if m, ok := f.items[id]; ok {
		return m, nil
	}
	return domain.SourceMap{}, domain.ErrNotFound
}
func (f *fakeSourceMapStore) ListSourceMaps(_ context.Context, app, release string) ([]domain.SourceMap, error) {
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
func (f *fakeSourceMapStore) GetSourceMapContent(_ context.Context, _, _, _ string) (string, error) {
	return "", nil
}
func (f *fakeSourceMapStore) DeleteSourceMap(_ context.Context, id string) error {
	if _, ok := f.items[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.items, id)
	return nil
}

func TestSourceMap_ListFiltraPorApp(t *testing.T) {
	store := newFakeSourceMapStore()
	_, _ = store.UpsertSourceMap(context.Background(), domain.SourceMap{App: "portal", Release: "1.0"}, "")
	_, _ = store.UpsertSourceMap(context.Background(), domain.SourceMap{App: "outro", Release: "1.0"}, "")
	svc := NewSourceMapService(store, slog.Default())
	got, err := svc.List(context.Background(), "portal", "")
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if len(got) != 1 || got[0].App != "portal" {
		t.Errorf("filtro não funcionou: %+v", got)
	}
}

func TestSourceMap_GetInexistenteRetornaNotFound(t *testing.T) {
	svc := NewSourceMapService(newFakeSourceMapStore(), slog.Default())
	_, err := svc.Get(context.Background(), "no-id")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("esperava NotFound, veio %v", err)
	}
}
