package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// fakeIssueReader devolve issues fixas — só o método FindIssues é usado.
type fakeIssueReader struct {
	nopEventReader
	issues []domain.Issue
}

func (r *fakeIssueReader) FindIssues(context.Context, domain.IssueFilter) ([]domain.Issue, error) {
	return r.issues, nil
}

// fakeIssueStore materializa states/comments/assignments em memória.
type fakeIssueStore struct {
	states   map[string]domain.IssueState
	comments map[string][]domain.IssueComment // fingerprint -> comments
	assignee map[string][]string              // userID -> fingerprints
	seq      int
}

func newFakeIssueStore() *fakeIssueStore {
	return &fakeIssueStore{
		states:   map[string]domain.IssueState{},
		comments: map[string][]domain.IssueComment{},
		assignee: map[string][]string{},
	}
}

func (f *fakeIssueStore) GetIssueStates(_ context.Context, fps []string) (map[string]domain.IssueState, error) {
	out := map[string]domain.IssueState{}
	for _, fp := range fps {
		if st, ok := f.states[fp]; ok {
			out[fp] = st
		}
	}
	return out, nil
}
func (f *fakeIssueStore) UpsertIssueStatus(_ context.Context, in domain.IssueStatusInput) error {
	f.states[in.Fingerprint] = domain.IssueState{
		Status:         in.Status,
		Assignee:       in.Assignee,
		AssigneeUserID: in.AssigneeUserID,
	}
	if in.AssigneeUserID != "" {
		f.assignee[in.AssigneeUserID] = append(f.assignee[in.AssigneeUserID], in.Fingerprint)
	}
	return nil
}
func (f *fakeIssueStore) ListIssueComments(_ context.Context, fp string) ([]domain.IssueComment, error) {
	return f.comments[fp], nil
}
func (f *fakeIssueStore) CreateIssueComment(_ context.Context, fp, authorID, body string) (domain.IssueComment, error) {
	f.seq++
	c := domain.IssueComment{
		ID: string(rune('A' + f.seq)), Fingerprint: fp, AuthorID: authorID, Body: body,
		CreatedAt: time.Now().UTC(),
	}
	f.comments[fp] = append(f.comments[fp], c)
	return c, nil
}
func (f *fakeIssueStore) DeleteIssueComment(_ context.Context, id, requesterID string) error {
	for fp, list := range f.comments {
		for i, c := range list {
			if c.ID == id {
				if c.AuthorID != requesterID {
					return domain.ErrForbidden
				}
				f.comments[fp] = append(list[:i], list[i+1:]...)
				return nil
			}
		}
	}
	return domain.ErrNotFound
}
func (f *fakeIssueStore) ListFingerprintsByAssignee(_ context.Context, userID string) ([]string, error) {
	return f.assignee[userID], nil
}

// -- IssueStatus.Valid -------------------------------------------------

func TestIssueStatus_Valid(t *testing.T) {
	for _, s := range []domain.IssueStatus{domain.IssueOpen, domain.IssueInvestigating, domain.IssueResolved, domain.IssueIgnored} {
		if !s.Valid() {
			t.Errorf("status %q deveria ser válido", s)
		}
	}
	for _, s := range []domain.IssueStatus{"", "wip", "closed"} {
		if s.Valid() {
			t.Errorf("status %q não deveria ser válido", s)
		}
	}
}

// -- List --------------------------------------------------------------

func setupIssueSvc(issues ...domain.Issue) (*IssueService, *fakeIssueStore) {
	store := newFakeIssueStore()
	reader := &fakeIssueReader{issues: issues}
	return NewIssueService(reader, store), store
}

func TestIssueService_ListSemStatesRetornaOpen(t *testing.T) {
	// Se não há registro em issue_states, default é "open".
	svc, _ := setupIssueSvc(
		domain.Issue{Fingerprint: "fp-1", ImpactScore: 10},
	)
	list, err := svc.List(context.Background(), ListInput{})
	if err != nil {
		t.Fatalf("list falhou: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("esperava 1 issue, veio %d", len(list))
	}
	if list[0].Status != string(domain.IssueOpen) {
		t.Errorf("status default deveria ser 'open', veio %q", list[0].Status)
	}
}

func TestIssueService_ListAnexaEstadoPersistido(t *testing.T) {
	svc, store := setupIssueSvc(
		domain.Issue{Fingerprint: "fp-1"},
	)
	store.states["fp-1"] = domain.IssueState{
		Status:         domain.IssueResolved,
		AssigneeUserID: "u-1",
		AssigneeName:   "Fulano",
		AssigneeEmail:  "f@x",
	}
	list, _ := svc.List(context.Background(), ListInput{})
	if list[0].Status != string(domain.IssueResolved) {
		t.Errorf("status errado: %q", list[0].Status)
	}
	if list[0].AssigneeName != "Fulano" {
		t.Errorf("assignee name não veio: %q", list[0].AssigneeName)
	}
}

func TestIssueService_ListOnlyOpenFiltraResolvidasEIgnored(t *testing.T) {
	svc, store := setupIssueSvc(
		domain.Issue{Fingerprint: "fp-open"},        // sem state = open
		domain.Issue{Fingerprint: "fp-investigating"},
		domain.Issue{Fingerprint: "fp-resolved"},
		domain.Issue{Fingerprint: "fp-ignored"},
	)
	store.states["fp-investigating"] = domain.IssueState{Status: domain.IssueInvestigating}
	store.states["fp-resolved"] = domain.IssueState{Status: domain.IssueResolved}
	store.states["fp-ignored"] = domain.IssueState{Status: domain.IssueIgnored}

	list, _ := svc.List(context.Background(), ListInput{OnlyOpen: true})
	got := map[string]bool{}
	for _, is := range list {
		got[is.Fingerprint] = true
	}
	if !got["fp-open"] || !got["fp-investigating"] {
		t.Errorf("open+investigating deveriam aparecer, got=%v", got)
	}
	if got["fp-resolved"] || got["fp-ignored"] {
		t.Errorf("resolved+ignored não deveriam aparecer, got=%v", got)
	}
}

func TestIssueService_ListAssigneeFiltraAtribuidas(t *testing.T) {
	svc, store := setupIssueSvc(
		domain.Issue{Fingerprint: "fp-mine"},
		domain.Issue{Fingerprint: "fp-outra"},
	)
	// Registra fp-mine como atribuída a u-1.
	_ = svc.SetStatus(context.Background(), domain.IssueStatusInput{
		Fingerprint: "fp-mine", Status: domain.IssueOpen, AssigneeUserID: "u-1",
	})
	// (usa store diretamente pra validar)
	_ = store

	list, _ := svc.List(context.Background(), ListInput{AssigneeID: "u-1"})
	if len(list) != 1 || list[0].Fingerprint != "fp-mine" {
		t.Errorf("esperava só fp-mine, veio %+v", list)
	}
}

func TestIssueService_ListOrdenaPorImpactScoreDesc(t *testing.T) {
	svc, _ := setupIssueSvc(
		domain.Issue{Fingerprint: "fp-low", ImpactScore: 1},
		domain.Issue{Fingerprint: "fp-high", ImpactScore: 100},
		domain.Issue{Fingerprint: "fp-mid", ImpactScore: 50},
	)
	list, _ := svc.List(context.Background(), ListInput{})
	if list[0].Fingerprint != "fp-high" || list[2].Fingerprint != "fp-low" {
		t.Errorf("ordenação errada: %+v", list)
	}
}

func TestIssueService_ListVazioNaoQuebra(t *testing.T) {
	svc, _ := setupIssueSvc()
	list, err := svc.List(context.Background(), ListInput{})
	if err != nil {
		t.Fatalf("list falhou em vazio: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("esperava vazio, veio %d", len(list))
	}
}

// -- SetStatus ---------------------------------------------------------

func TestIssueService_SetStatusValida(t *testing.T) {
	svc, _ := setupIssueSvc()
	cases := []struct {
		name string
		in   domain.IssueStatusInput
	}{
		{"fingerprint vazio", domain.IssueStatusInput{Status: domain.IssueOpen}},
		{"status inválido", domain.IssueStatusInput{Fingerprint: "fp", Status: "algum"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := svc.SetStatus(context.Background(), c.in)
			if err == nil {
				t.Fatal("esperava erro de validação")
			}
		})
	}
}

func TestIssueService_SetStatusPersiste(t *testing.T) {
	svc, store := setupIssueSvc()
	err := svc.SetStatus(context.Background(), domain.IssueStatusInput{
		Fingerprint: "fp-1", Status: domain.IssueResolved, AssigneeUserID: "u-1",
	})
	if err != nil {
		t.Fatalf("setStatus falhou: %v", err)
	}
	st := store.states["fp-1"]
	if st.Status != domain.IssueResolved {
		t.Errorf("status não persistiu: %q", st.Status)
	}
	if st.AssigneeUserID != "u-1" {
		t.Errorf("assignee não persistiu: %q", st.AssigneeUserID)
	}
}

// -- Comments ---------------------------------------------------------

func TestIssueService_AddCommentValida(t *testing.T) {
	svc, _ := setupIssueSvc()
	cases := []struct {
		name string
		fp   string
		body string
	}{
		{"fingerprint vazio", "", "texto"},
		{"body vazio", "fp", ""},
		{"body só espaço", "fp", "   "},
		{"body > 5000", "fp", strings.Repeat("x", 5001)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := svc.AddComment(context.Background(), c.fp, "u-1", c.body)
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

func TestIssueService_AddCommentTrim(t *testing.T) {
	svc, store := setupIssueSvc()
	c, err := svc.AddComment(context.Background(), "fp-1", "u-1", "  olá  ")
	if err != nil {
		t.Fatalf("add falhou: %v", err)
	}
	if c.Body != "olá" {
		t.Errorf("body não trimmed: %q", c.Body)
	}
	if len(store.comments["fp-1"]) != 1 {
		t.Errorf("comment não persistiu")
	}
}

func TestIssueService_DeleteCommentSoAutor(t *testing.T) {
	svc, _ := setupIssueSvc()
	c, _ := svc.AddComment(context.Background(), "fp-1", "u-autor", "hi")

	// Outro usuário tenta apagar → ErrForbidden.
	err := svc.DeleteComment(context.Background(), c.ID, "u-outro")
	if !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("esperava Forbidden pra não-autor, veio %v", err)
	}

	// Autor apaga → OK.
	if err := svc.DeleteComment(context.Background(), c.ID, "u-autor"); err != nil {
		t.Errorf("autor deveria conseguir apagar: %v", err)
	}
}

func TestIssueService_ListCommentsFingerprintVazioErro(t *testing.T) {
	svc, _ := setupIssueSvc()
	_, err := svc.ListComments(context.Background(), "")
	if err == nil {
		t.Fatal("esperava erro")
	}
}
