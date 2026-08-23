package usecase

import (
	"context"
	"sort"
	"strings"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// IssueService compõe o agrupamento de erros (ClickHouse) com o estado
// mutável (Postgres): a lista de issues já vem com status/assignee/comentários
// anexados e o dashboard pode filtrar por "open" ou "atribuídas a mim" sem
// consultar duas fontes.
type IssueService struct {
	reader domain.EventReader
	states domain.IssueStore
}

// NewIssueService cria o serviço de issues.
func NewIssueService(reader domain.EventReader, states domain.IssueStore) *IssueService {
	return &IssueService{reader: reader, states: states}
}

// ListInput controla o filtro da listagem.
type ListInput struct {
	Filter     domain.IssueFilter
	OnlyOpen   bool
	AssigneeID string // vazio = todos; não-vazio = "atribuídas a mim"
}

// List agrupa erros por fingerprint e anexa o estado persistido. Filtra
// por status/assignee se pedido — quando AssigneeID vem preenchido, restringe
// aos fingerprints que aquele usuário é dono.
func (s *IssueService) List(ctx context.Context, in ListInput) ([]domain.Issue, error) {
	issues, err := s.reader.FindIssues(ctx, in.Filter)
	if err != nil {
		return nil, err
	}
	if len(issues) == 0 {
		return issues, nil
	}
	fps := make([]string, len(issues))
	for i, is := range issues {
		fps[i] = is.Fingerprint
	}
	states, err := s.states.GetIssueStates(ctx, fps)
	if err != nil {
		return nil, err
	}

	// Filtro "atribuídas a mim": pré-carrega o set do usuário e restringe.
	assigneeSet := map[string]struct{}{}
	if in.AssigneeID != "" {
		mine, err := s.states.ListFingerprintsByAssignee(ctx, in.AssigneeID)
		if err != nil {
			return nil, err
		}
		for _, fp := range mine {
			assigneeSet[fp] = struct{}{}
		}
	}

	out := make([]domain.Issue, 0, len(issues))
	for _, is := range issues {
		if st, ok := states[is.Fingerprint]; ok {
			is.Status = string(st.Status)
			is.Assignee = st.Assignee
			is.AssigneeUserID = st.AssigneeUserID
			is.AssigneeName = st.AssigneeName
			is.AssigneeEmail = st.AssigneeEmail
		} else {
			is.Status = string(domain.IssueOpen)
		}
		if in.OnlyOpen && is.Status != string(domain.IssueOpen) &&
			is.Status != string(domain.IssueInvestigating) {
			continue
		}
		if in.AssigneeID != "" {
			if _, mine := assigneeSet[is.Fingerprint]; !mine {
				continue
			}
		}
		out = append(out, is)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ImpactScore > out[j].ImpactScore })
	return out, nil
}

// SetStatus atualiza o estado de uma issue (status + assignee via user_id).
func (s *IssueService) SetStatus(ctx context.Context, in domain.IssueStatusInput) error {
	if strings.TrimSpace(in.Fingerprint) == "" {
		return domain.NewValidationError("fingerprint é obrigatório")
	}
	if !in.Status.Valid() {
		return domain.NewValidationError("status inválido")
	}
	return s.states.UpsertIssueStatus(ctx, in)
}

// ListComments devolve os comentários (mais recentes primeiro).
func (s *IssueService) ListComments(ctx context.Context, fingerprint string) ([]domain.IssueComment, error) {
	if fingerprint == "" {
		return nil, domain.NewValidationError("fingerprint é obrigatório")
	}
	return s.states.ListIssueComments(ctx, fingerprint)
}

// AddComment cria um comentário atribuído ao autor identificado.
func (s *IssueService) AddComment(ctx context.Context, fingerprint, authorID, body string) (domain.IssueComment, error) {
	if fingerprint == "" {
		return domain.IssueComment{}, domain.NewValidationError("fingerprint é obrigatório")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return domain.IssueComment{}, domain.NewValidationError("comentário não pode ser vazio")
	}
	if len(body) > 5000 {
		return domain.IssueComment{}, domain.NewValidationError("comentário excede 5000 caracteres")
	}
	return s.states.CreateIssueComment(ctx, fingerprint, authorID, body)
}

// DeleteComment só permite o próprio autor apagar.
func (s *IssueService) DeleteComment(ctx context.Context, id, requesterID string) error {
	if id == "" {
		return domain.NewValidationError("id é obrigatório")
	}
	return s.states.DeleteIssueComment(ctx, id, requesterID)
}
