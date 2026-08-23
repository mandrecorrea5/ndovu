package domain

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strings"
	"time"
)

// numRe normaliza números embutidos em mensagens de erro para que a mesma
// falha com IDs diferentes agrupe no mesmo fingerprint.
var numRe = regexp.MustCompile(`\d+`)

// urlPathRe substitui segmentos numéricos/UUIDs em rotas por :id para
// agrupar erros da mesma rota independentemente do parâmetro.
var urlPathRe = regexp.MustCompile(`/[0-9a-f-]{8,}`)

// Fingerprint gera um hash estável que agrupa erros parecidos em uma issue.
// Considera: app + tipo + code + rota normalizada + mensagem normalizada.
// Eventos sem erro retornam string vazia (não são issues).
func Fingerprint(e TraceEvent) string {
	if !e.HasError() && e.Type != EventError {
		return ""
	}

	var parts []string
	parts = append(parts, e.App)
	parts = append(parts, string(e.Type))

	if e.Error != nil {
		parts = append(parts, strings.ToUpper(strings.TrimSpace(e.Error.Code)))
		parts = append(parts, normalizeErrorMessage(e.Error.Message))
	}

	// Rota importa mais que URL crua: /users/123 e /users/456 são a mesma issue.
	if e.HTTP != nil && e.HTTP.URL != "" {
		parts = append(parts, e.HTTP.Method+" "+normalizeRoute(e.HTTP.URL))
	}

	// Fallback pra erros sem code/msg: usa o nome do evento.
	if e.Error == nil || (e.Error.Code == "" && e.Error.Message == "") {
		parts = append(parts, e.Name)
	}

	sum := sha1.Sum([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])[:16]
}

func normalizeErrorMessage(msg string) string {
	if msg == "" {
		return ""
	}
	// Remove IDs e números para agrupar variantes da mesma falha.
	s := numRe.ReplaceAllString(msg, "N")
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		s = s[:200]
	}
	return strings.ToLower(s)
}

func normalizeRoute(url string) string {
	// Corta querystring
	if i := strings.IndexByte(url, '?'); i >= 0 {
		url = url[:i]
	}
	return urlPathRe.ReplaceAllString(url, "/:id")
}

// Issue é um erro agrupado por fingerprint dentro de uma janela.
// A fonte de verdade dos eventos é o ClickHouse; o Postgres guarda
// apenas o estado mutável (status, assignee, first_seen resolvido).
type Issue struct {
	Fingerprint     string  `json:"fingerprint"`
	App             string  `json:"app"`
	Type            string  `json:"type"`
	Code            string  `json:"code"`
	Message         string  `json:"message"`
	Name            string  `json:"name"`
	SampleURL       string  `json:"sampleUrl,omitempty"`
	Count           int     `json:"count"`
	AffectedUsers   int     `json:"affectedUsers"`
	FirstSeen       string  `json:"firstSeen"`
	LastSeen        string  `json:"lastSeen"`
	Status          string  `json:"status,omitempty"`
	Assignee        string  `json:"assignee,omitempty"`         // texto legado
	AssigneeUserID  string  `json:"assigneeUserId,omitempty"`   // FK preferido
	AssigneeName    string  `json:"assigneeName,omitempty"`     // resolvido via join
	AssigneeEmail   string  `json:"assigneeEmail,omitempty"`
	ImpactScore     float64 `json:"impactScore"`
}

// IssueComment é um comentário deixado por um usuário do backoffice numa issue.
// O corpo é texto livre (markdown-friendly).
type IssueComment struct {
	ID          string    `json:"id"`
	Fingerprint string    `json:"fingerprint"`
	AuthorID    string    `json:"authorId,omitempty"`
	AuthorName  string    `json:"authorName,omitempty"`
	AuthorEmail string    `json:"authorEmail,omitempty"`
	Body        string    `json:"body"`
	CreatedAt   time.Time `json:"createdAt"`
}

// IssueStatus representa o estado de triagem de uma issue.
type IssueStatus string

const (
	IssueOpen          IssueStatus = "open"
	IssueInvestigating IssueStatus = "investigating"
	IssueResolved      IssueStatus = "resolved"
	IssueIgnored       IssueStatus = "ignored"
)

// Valid retorna true se o status é reconhecido.
func (s IssueStatus) Valid() bool {
	switch s {
	case IssueOpen, IssueInvestigating, IssueResolved, IssueIgnored:
		return true
	}
	return false
}
