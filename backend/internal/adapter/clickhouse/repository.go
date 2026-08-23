// Package clickhouse implementa os ports de persistência sobre ClickHouse —
// o armazém analítico de traces (colunar, particionado por dia, TTL de retenção).
package clickhouse

import (
	"context"
	"crypto/tls"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

//go:embed schema.sql
var schemaSQL string

// Options configura a conexão.
type Options struct {
	Addr     string // host:porta (protocolo nativo, ex.: clickhouse:9000)
	Database string
	Username string
	Password string
	Secure   bool
}

// Repository implementa domain.EventWriter e domain.EventReader.
type Repository struct {
	conn driver.Conn
}

// Connect abre a conexão nativa, aguardando o servidor subir (compose).
func Connect(ctx context.Context, opts Options) (*Repository, error) {
	var tlsCfg *tls.Config
	if opts.Secure {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{opts.Addr},
		Auth: clickhouse.Auth{
			Database: opts.Database,
			Username: opts.Username,
			Password: opts.Password,
		},
		TLS:             tlsCfg,
		DialTimeout:     5 * time.Second,
		MaxOpenConns:    10,
		ConnMaxLifetime: 30 * time.Minute,
		Compression:     &clickhouse.Compression{Method: clickhouse.CompressionLZ4},
	})
	if err != nil {
		return nil, fmt.Errorf("abrindo conexão clickhouse: %w", err)
	}

	deadline := time.Now().Add(90 * time.Second)
	for {
		if err = conn.Ping(ctx); err == nil {
			return &Repository{conn: conn}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("clickhouse indisponível após 90s: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// Close encerra a conexão.
func (r *Repository) Close() error { return r.conn.Close() }

// EnsureSchema aplica o DDL embedado (idempotente: IF NOT EXISTS).
// Remove comentários ANTES do split por ";" — caso contrário um ";" dentro
// de um comentário quebra o statement no meio.
func (r *Repository) EnsureSchema(ctx context.Context) error {
	cleaned := stripComments(schemaSQL)
	for _, stmt := range strings.Split(cleaned, ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if err := r.conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("aplicando schema: %w", err)
		}
	}
	return nil
}

func stripComments(sql string) string {
	var b strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "--") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Escrita (writer) — bulk insert de vários lotes
// ---------------------------------------------------------------------------

// SaveBatches insere todos os eventos de todos os lotes em um único INSERT —
// o formato que o ClickHouse mais gosta. A deduplicação por id acontece no
// engine (ReplacingMergeTree), então reentregas do stream são seguras.
func (r *Repository) SaveBatches(ctx context.Context, batches []domain.IngestBatch) (domain.IngestResult, error) {
	batch, err := r.conn.PrepareBatch(ctx, `INSERT INTO trace_events
		(id, session_id, user_id, app, release, event_type, name, feature, screen,
		 http_method, http_url, http_status, duration_ms,
		 request_body, response_body, error_code, error_message, error_body,
		 metadata, user_agent, session_attrs, occurred_at, received_at,
		 trace_id, span_id, parent_span_id)`)
	if err != nil {
		return domain.IngestResult{}, fmt.Errorf("preparando insert: %w", err)
	}

	total := 0
	for _, b := range batches {
		userAgent := b.Session.UserAgent
		attrs := string(rawOrEmptyObject(b.Session.Attributes))
		for _, e := range b.Events {
			var method, url string
			var status *uint16
			var reqBody, respBody string
			if e.HTTP != nil {
				method, url = e.HTTP.Method, e.HTTP.URL
				if e.HTTP.StatusCode != nil {
					s := uint16(*e.HTTP.StatusCode) //nolint:gosec // status HTTP cabe em uint16
					status = &s
				}
				reqBody = string(e.HTTP.RequestBody)
				respBody = string(e.HTTP.ResponseBody)
			}
			var errCode, errMsg, errBody string
			if e.Error != nil {
				errCode, errMsg = e.Error.Code, e.Error.Message
				errBody = string(e.Error.Body)
			}
			var duration *int32
			if e.DurationMs != nil {
				d := int32(*e.DurationMs) //nolint:gosec // durações reais cabem em int32
				duration = &d
			}

			traceID, spanID, parentSpanID := e.TraceIDs()
			if err := batch.Append(
				e.ID, e.SessionID, e.UserID, e.App, e.Release, string(e.Type), e.Name, e.Feature, e.Screen,
				method, url, status, duration,
				reqBody, respBody, errCode, errMsg, errBody,
				string(e.Metadata), userAgent, attrs, e.OccurredAt, e.ReceivedAt,
				traceID, spanID, parentSpanID,
			); err != nil {
				return domain.IngestResult{}, fmt.Errorf("montando linha: %w", err)
			}
			total++
		}
	}

	if err := batch.Send(); err != nil {
		return domain.IngestResult{}, fmt.Errorf("enviando insert: %w", err)
	}
	return domain.IngestResult{Accepted: total}, nil
}

// ---------------------------------------------------------------------------
// Leitura (consulta) — read-only
// ---------------------------------------------------------------------------

const eventColumns = `
	toString(id), session_id, user_id, app, release, event_type, name, feature, screen,
	http_method, http_url, http_status, duration_ms,
	request_body, response_body, error_code, error_message, error_body,
	metadata, occurred_at, received_at,
	trace_id, span_id, parent_span_id`

const errorCond = `(error_code != '' OR error_message != '' OR event_type = 'error')`

// FindEvents monta a consulta dinâmica com SQL sempre parametrizado.
func (r *Repository) FindEvents(ctx context.Context, f domain.EventFilter) (domain.EventPage, error) {
	q := newQueryBuilder("SELECT" + eventColumns + " FROM trace_events FINAL")

	q.whereTime("occurred_at", f.From, f.To)
	q.whereEq("app", f.App)
	q.whereEq("release", f.Release)
	q.whereInStr("app", f.AppScope)
	q.whereEq("user_id", f.UserID)
	q.whereEq("session_id", f.SessionID)
	q.whereEq("event_type", string(f.Type))
	q.whereEq("feature", f.Feature)
	q.whereEq("name", f.Name)
	q.whereEq("screen", f.Screen)
	if f.HTTPURL != "" {
		q.where("positionCaseInsensitive(http_url, " + q.arg(f.HTTPURL) + ") > 0")
	}
	if f.StatusMin != nil {
		q.where("http_status >= " + q.arg(*f.StatusMin))
	}
	if f.StatusMax != nil {
		q.where("http_status <= " + q.arg(*f.StatusMax))
	}
	if f.OnlyErrors {
		q.where(errorCond)
	}
	if f.Search != "" {
		p := q.arg(f.Search)
		q.where(fmt.Sprintf(`(positionCaseInsensitive(name, %[1]s) > 0
			OR positionCaseInsensitive(screen, %[1]s) > 0
			OR positionCaseInsensitive(http_url, %[1]s) > 0
			OR positionCaseInsensitive(error_code, %[1]s) > 0
			OR positionCaseInsensitive(error_message, %[1]s) > 0)`, p))
	}
	if f.Cursor != nil {
		t, id := q.arg(f.Cursor.OccurredAt), q.arg(f.Cursor.ID)
		q.where(fmt.Sprintf("(occurred_at < %[1]s OR (occurred_at = %[1]s AND toString(id) < %[2]s))", t, id))
	}
	// toString(id) no desempate mantém a ordenação consistente com o cursor,
	// que compara ids como string.
	q.orderBy("occurred_at DESC, toString(id) DESC")
	q.limit(f.Limit + 1)

	rows, err := r.conn.Query(ctx, q.sql(), q.args...)
	if err != nil {
		return domain.EventPage{}, fmt.Errorf("consultando eventos: %w", err)
	}
	defer rows.Close()

	events, err := scanEvents(rows)
	if err != nil {
		return domain.EventPage{}, err
	}

	page := domain.EventPage{Events: events}
	if len(events) > f.Limit {
		page.Events = events[:f.Limit]
		last := page.Events[len(page.Events)-1]
		page.NextCursor = EncodeCursor(last.OccurredAt, last.ID)
	}
	return page, nil
}

// GetEvent retorna um evento por id.
func (r *Repository) GetEvent(ctx context.Context, id string) (domain.TraceEvent, error) {
	rows, err := r.conn.Query(ctx,
		"SELECT"+eventColumns+" FROM trace_events FINAL WHERE toString(id) = $1 LIMIT 1", id)
	if err != nil {
		return domain.TraceEvent{}, fmt.Errorf("consultando evento: %w", err)
	}
	defer rows.Close()
	events, err := scanEvents(rows)
	if err != nil {
		return domain.TraceEvent{}, err
	}
	if len(events) == 0 {
		return domain.TraceEvent{}, domain.ErrNotFound
	}
	return events[0], nil
}

// Aliases com sufixo _agg evitam colisão do ClickHouse quando o WHERE do
// tenantScope reusa o mesmo nome da coluna original — o parser interpretaria
// `WHERE app = X` como referência ao alias agregado (any(app) AS app) e
// falharia com "Aggregate function ... is found in WHERE in query".
const sessionAggSelect = `
	SELECT session_id,
	       max(user_id) AS user_id_agg,
	       any(app) AS app_agg,
	       max(user_agent) AS user_agent_agg,
	       max(session_attrs) AS attributes_agg,
	       min(occurred_at) AS started_at,
	       max(occurred_at) AS last_event_at,
	       toUInt64(count()) AS event_count,
	       toUInt64(countIf` + `(error_code != '' OR error_message != '' OR event_type = 'error')) AS error_count
	FROM trace_events FINAL`

// FindSessions lista sessões agregadas a partir dos eventos (sempre consistente
// com o que está deduplicado no armazém).
func (r *Repository) FindSessions(ctx context.Context, f domain.SessionFilter) (domain.SessionPage, error) {
	inner := newQueryBuilder(sessionAggSelect)
	inner.whereEq("app", f.App)
	inner.whereInStr("app", f.AppScope)
	inner.whereEq("user_id", f.UserID)
	inner.groupBy("session_id")
	if f.From != nil {
		inner.having("last_event_at >= " + inner.arg(*f.From))
	}
	if f.To != nil {
		inner.having("last_event_at < " + inner.arg(*f.To))
	}
	if f.OnlyErrors {
		inner.having("error_count > 0")
	}

	sql := "SELECT *, toUInt64(count() OVER ()) AS total FROM (" + inner.sql() + ") ORDER BY last_event_at DESC"
	sql += fmt.Sprintf(" LIMIT %d OFFSET %d", f.Limit, f.Offset)

	rows, err := r.conn.Query(ctx, sql, inner.args...)
	if err != nil {
		return domain.SessionPage{}, fmt.Errorf("consultando sessões: %w", err)
	}
	defer rows.Close()

	page := domain.SessionPage{Sessions: []domain.Session{}}
	for rows.Next() {
		s, total, err := scanSession(rows, true)
		if err != nil {
			return domain.SessionPage{}, err
		}
		page.Total = total
		page.Sessions = append(page.Sessions, s)
	}
	return page, rows.Err()
}

// GetSession retorna uma sessão agregada por id.
func (r *Repository) GetSession(ctx context.Context, sessionID string) (domain.Session, error) {
	q := newQueryBuilder(sessionAggSelect)
	q.whereEq("session_id", sessionID)
	q.groupBy("session_id")

	rows, err := r.conn.Query(ctx, q.sql(), q.args...)
	if err != nil {
		return domain.Session{}, fmt.Errorf("consultando sessão: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return domain.Session{}, domain.ErrNotFound
	}
	s, _, err := scanSession(rows, false)
	return s, err
}

// SessionTimeline retorna o rastro cronológico completo de uma sessão.
// A projection by_session garante que isso não varre a janela inteira.
func (r *Repository) SessionTimeline(ctx context.Context, sessionID string) ([]domain.TraceEvent, error) {
	rows, err := r.conn.Query(ctx,
		"SELECT"+eventColumns+" FROM trace_events FINAL WHERE session_id = $1 ORDER BY occurred_at ASC, received_at ASC",
		sessionID)
	if err != nil {
		return nil, fmt.Errorf("consultando timeline: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

// FindEventsByUser retorna todos os eventos de um user (LGPD: portabilidade).
// Limitado a 10k eventos para não estourar memória — o endpoint pagina se
// necessário. Ordena por occurred_at ASC (histórico cronológico).
func (r *Repository) FindEventsByUser(ctx context.Context, userID string) ([]domain.TraceEvent, error) {
	if userID == "" {
		return nil, fmt.Errorf("userID vazio")
	}
	rows, err := r.conn.Query(ctx,
		"SELECT"+eventColumns+" FROM trace_events FINAL WHERE user_id = $1 ORDER BY occurred_at ASC LIMIT 10000",
		userID)
	if err != nil {
		return nil, fmt.Errorf("consultando eventos por user: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

// DeleteEventsByUser apaga TODOS os eventos de um user (LGPD: direito ao
// esquecimento). Usa ALTER TABLE ... DELETE — assíncrono no ClickHouse:
// a mutation entra na fila do sistema; consultas subsequentes podem ainda
// ver os dados por alguns segundos até a merge propagar. Aceitável para o
// caso de uso (o registro de exclusão fica no audit log).
func (r *Repository) DeleteEventsByUser(ctx context.Context, userID string) error {
	if userID == "" {
		return fmt.Errorf("userID vazio")
	}
	// ClickHouse não aceita placeholder posicional em ALTER; a validação
	// acima + o quote(...) manual previnem injeção.
	sql := fmt.Sprintf(
		"ALTER TABLE trace_events DELETE WHERE user_id = %s",
		quote(userID),
	)
	if err := r.conn.Exec(ctx, sql); err != nil {
		return fmt.Errorf("apagando eventos: %w", err)
	}
	return nil
}

// TraceTimeline retorna todos os eventos que compartilham o mesmo W3C trace_id,
// ordenados no tempo. Base do endpoint /v1/traces/{traceId} — mostra frontend
// e backend correlacionados em uma única timeline.
func (r *Repository) TraceTimeline(ctx context.Context, traceID string) ([]domain.TraceEvent, error) {
	if traceID == "" {
		return nil, nil
	}
	rows, err := r.conn.Query(ctx,
		"SELECT"+eventColumns+" FROM trace_events FINAL WHERE trace_id = $1 ORDER BY occurred_at ASC, received_at ASC",
		traceID)
	if err != nil {
		return nil, fmt.Errorf("consultando trace: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

// GetOverview agrega métricas da janela: totais, série temporal, top rotas e erros.
func (r *Repository) GetOverview(ctx context.Context, from, to time.Time, app string) (domain.Overview, error) {
	o := domain.Overview{
		Series:    []domain.TimeBucket{},
		TopRoutes: []domain.RouteStat{},
		TopErrors: []domain.ErrorStat{},
	}
	appFilter := " AND ($3 = '' OR app = $3)"

	var totalEvents, totalSessions, totalUsers, totalErrors uint64
	var avgMs float64
	err := r.conn.QueryRow(ctx, `
		SELECT count() AS total_events,
		       uniqExact(session_id) AS total_sessions,
		       uniqExactIf(user_id, user_id != '') AS total_users,
		       countIf`+errorCond+` AS total_errors,
		       coalesce(avg(duration_ms), 0) AS avg_ms
		FROM trace_events FINAL
		WHERE occurred_at >= $1 AND occurred_at < $2`+appFilter,
		from, to, app).
		Scan(&totalEvents, &totalSessions, &totalUsers, &totalErrors, &avgMs)
	if err != nil {
		return o, fmt.Errorf("agregando totais: %w", err)
	}
	o.TotalEvents = int(totalEvents)     //nolint:gosec
	o.TotalSessions = int(totalSessions) //nolint:gosec
	o.TotalUsers = int(totalUsers)       //nolint:gosec
	o.TotalErrors = int(totalErrors)     //nolint:gosec
	o.AvgDurationMs = avgMs
	if o.TotalEvents > 0 {
		o.ErrorRate = float64(o.TotalErrors) / float64(o.TotalEvents)
	}

	bucket := int64(adaptiveBucket(from, to).Seconds())
	rows, err := r.conn.Query(ctx, fmt.Sprintf(`
		SELECT toStartOfInterval(occurred_at, INTERVAL %d SECOND) AS bucket,
		       toUInt64(count()) AS total,
		       toUInt64(countIf%s) AS errors
		FROM trace_events FINAL
		WHERE occurred_at >= $1 AND occurred_at < $2%s
		GROUP BY bucket ORDER BY bucket`, bucket, errorCond, appFilter),
		from, to, app)
	if err != nil {
		return o, fmt.Errorf("agregando série: %w", err)
	}
	for rows.Next() {
		var tb domain.TimeBucket
		var total, errs uint64
		if err := rows.Scan(&tb.Bucket, &total, &errs); err != nil {
			rows.Close()
			return o, err
		}
		tb.Total, tb.Errors = int(total), int(errs) //nolint:gosec
		o.Series = append(o.Series, tb)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return o, err
	}

	rows, err = r.conn.Query(ctx, `
		SELECT http_url,
		       toUInt64(count()) AS c,
		       toUInt64(countIf(error_code != '' OR error_message != '')) AS errs,
		       coalesce(avg(duration_ms), 0) AS avg_ms,
		       coalesce(quantile(0.95)(duration_ms), 0) AS p95_ms
		FROM trace_events FINAL
		WHERE occurred_at >= $1 AND occurred_at < $2 AND http_url != ''`+appFilter+`
		GROUP BY http_url ORDER BY c DESC LIMIT 10`,
		from, to, app)
	if err != nil {
		return o, fmt.Errorf("agregando rotas: %w", err)
	}
	for rows.Next() {
		var rs domain.RouteStat
		var c, errs uint64
		if err := rows.Scan(&rs.URL, &c, &errs, &rs.AvgMs, &rs.P95Ms); err != nil {
			rows.Close()
			return o, err
		}
		rs.Count, rs.Errors = int(c), int(errs) //nolint:gosec
		o.TopRoutes = append(o.TopRoutes, rs)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return o, err
	}

	rows, err = r.conn.Query(ctx, `
		SELECT if(error_code = '', 'sem_codigo', error_code) AS code,
		       anyIf(error_message, error_message != '') AS msg,
		       toUInt64(count()) AS c
		FROM trace_events FINAL
		WHERE (error_code != '' OR error_message != '')
		  AND occurred_at >= $1 AND occurred_at < $2`+appFilter+`
		GROUP BY code ORDER BY c DESC LIMIT 10`,
		from, to, app)
	if err != nil {
		return o, fmt.Errorf("agregando erros: %w", err)
	}
	for rows.Next() {
		var es domain.ErrorStat
		var c uint64
		if err := rows.Scan(&es.Code, &es.Message, &c); err != nil {
			rows.Close()
			return o, err
		}
		es.Count = int(c) //nolint:gosec
		o.TopErrors = append(o.TopErrors, es)
	}
	rows.Close()
	return o, rows.Err()
}

// ---------------------------------------------------------------------------
// Issues (agrupamento de erros por fingerprint) e Web Vitals
// ---------------------------------------------------------------------------

// fingerprintSQL replica em SQL a normalização do domain.Fingerprint:
// concatena app|type|CODE_UPPER|msg_normalizada|METHOD rota_normalizada
// e devolve os primeiros 16 chars de sha1(hex).
// Precisamos manter isso alinhado com domain/fingerprint.go — mudanças em
// uma das pontas quebram o agrupamento até um novo backfill.
//
// Notas de compatibilidade com clickhouse-go/v2:
//   - O regex "[0-9a-f-]{8,}" tem {8,} que o parser do driver confunde com
//     placeholder nomeado ({name:Type}). Colocamos o padrão em uma constante
//     de string CONCATENADA em runtime para evitar essa interpretação.
//   - Envolvemos as colunas LowCardinality em CAST(... AS String) porque o if()
//     exige que "then" e "else" tenham exatamente o mesmo tipo.
const idPatternExpr = `concat('/[0-9a-f-]', '{8,}')`

var fingerprintSQL = `substring(hex(SHA1(concat(
    CAST(app AS String), '|',
    CAST(event_type AS String), '|',
    upper(error_code), '|',
    lower(replaceRegexpAll(error_message, '[0-9]+', 'N')), '|',
    if(http_url != '',
       concat(CAST(http_method AS String), ' ',
              replaceRegexpAll(splitByChar('?', http_url)[1], ` + idPatternExpr + `, '/:id')),
       CAST(name AS String))
))), 1, 16)`

// FindIssues agrupa erros por fingerprint em uma janela de tempo. Retorna
// contagem, usuários afetados, primeira/última ocorrência e um exemplo.
// A "impact score" combina volume, usuários e recência (log-scaled).
func (r *Repository) FindIssues(ctx context.Context, f domain.IssueFilter) ([]domain.Issue, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	from := time.Now().Add(-24 * time.Hour).UTC()
	to := time.Now().UTC()
	if f.From != nil {
		from = *f.From
	}
	if f.To != nil {
		to = *f.To
	}

	// Usamos named params (@name) porque o SQL do fingerprint tem {8,} embutido
	// em outra função — misturar $1/$2 com {} confunde o parser do driver.
	// AppScope é injetado como IN literal (whitelist controlada; não vem de
	// user input) porque o driver não aceita array como named param.
	scopeCond := ""
	if len(f.AppScope) > 0 {
		quoted := make([]string, len(f.AppScope))
		for i, v := range f.AppScope {
			quoted[i] = "'" + strings.ReplaceAll(v, "'", "''") + "'"
		}
		scopeCond = " AND app IN (" + strings.Join(quoted, ", ") + ")"
	}

	// Aliases têm sufixo _agg para não colidirem com as colunas cruas usadas
	// no WHERE (ex.: `any(app) AS app` faria o WHERE resolver `app` para o
	// aggregate em vez da coluna, o que o ClickHouse rejeita).
	query := fmt.Sprintf(`
		SELECT
			%s AS fp,
			any(app) AS app_agg,
			any(event_type) AS type_agg,
			anyIf(error_code, error_code != '') AS code_agg,
			anyIf(error_message, error_message != '') AS message_agg,
			any(name) AS name_agg,
			anyIf(http_url, http_url != '') AS sample_url,
			toUInt64(count()) AS c,
			toUInt64(uniqExactIf(user_id, user_id != '')) AS affected_users,
			min(occurred_at) AS first_seen,
			max(occurred_at) AS last_seen
		FROM trace_events FINAL
		WHERE (error_code != '' OR error_message != '' OR event_type = 'error')
		  AND occurred_at >= @from AND occurred_at < @to
		  AND (@app = '' OR app = @app)
		  AND (@release = '' OR release = @release)%s
		GROUP BY fp
		ORDER BY c DESC
		LIMIT %d`, fingerprintSQL, scopeCond, f.Limit)

	rows, err := r.conn.Query(ctx, query,
		clickhouse.Named("from", from),
		clickhouse.Named("to", to),
		clickhouse.Named("app", f.App),
		clickhouse.Named("release", f.Release),
	)
	if err != nil {
		return nil, fmt.Errorf("agrupando issues: %w", err)
	}
	defer rows.Close()

	issues := []domain.Issue{}
	for rows.Next() {
		var issue domain.Issue
		var count, users uint64
		var firstSeen, lastSeen time.Time
		if err := rows.Scan(&issue.Fingerprint, &issue.App, &issue.Type,
			&issue.Code, &issue.Message, &issue.Name, &issue.SampleURL,
			&count, &users, &firstSeen, &lastSeen); err != nil {
			return nil, fmt.Errorf("lendo issue: %w", err)
		}
		issue.Count = int(count)                //nolint:gosec
		issue.AffectedUsers = int(users)        //nolint:gosec
		issue.FirstSeen = firstSeen.UTC().Format(time.RFC3339)
		issue.LastSeen = lastSeen.UTC().Format(time.RFC3339)
		issue.ImpactScore = impactScore(issue.Count, issue.AffectedUsers, lastSeen)
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// impactScore combina volume, usuários afetados e recência para ordenar
// as issues por impacto real (não só por contagem crua).
func impactScore(count, users int, lastSeen time.Time) float64 {
	ageHours := time.Since(lastSeen).Hours()
	recency := 1.0 / (1.0 + ageHours/24.0) // decai ao longo de dias
	// log10(count+1) evita que 1 issue com 10k eventos domine todas as outras
	base := math.Log10(float64(count) + 1)
	userWeight := 1.0 + float64(users)
	return base * userWeight * recency
}

// MetricInWindow calcula error_count / event_count / error_rate numa janela.
// Usado pelo AnomalyService para amostrar o "agora" e cada réplica histórica
// (mesma hora+weekday nas semanas passadas).
func (r *Repository) MetricInWindow(ctx context.Context, metric, app string, from, to time.Time) (float64, error) {
	var expr string
	switch metric {
	case "error_count":
		expr = "toFloat64(countIf" + errorCond + ")"
	case "event_count":
		expr = "toFloat64(count())"
	case "error_rate":
		// Evita divisão por zero — se count=0, error_rate=0 (não é anomalia).
		expr = "if(count() > 0, countIf" + errorCond + " / count(), 0)"
	default:
		return 0, fmt.Errorf("métrica desconhecida: %s", metric)
	}
	var v float64
	err := r.conn.QueryRow(ctx, `
		SELECT `+expr+`
		FROM trace_events FINAL
		WHERE occurred_at >= $1 AND occurred_at < $2
		  AND ($3 = '' OR app = $3)`,
		from, to, app).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("medindo %s: %w", metric, err)
	}
	return v, nil
}

// CountErrorsSince conta erros de um code em uma janela — usado pelo avaliador
// de alertas para decidir se dispara notificação.
func (r *Repository) CountErrorsSince(ctx context.Context, app, code string, since time.Time) (int, error) {
	var count uint64
	err := r.conn.QueryRow(ctx, `
		SELECT toUInt64(count())
		FROM trace_events FINAL
		WHERE occurred_at >= $1
		  AND (error_code != '' OR error_message != '' OR event_type = 'error')
		  AND ($2 = '' OR app = $2)
		  AND ($3 = '' OR error_code = $3)`,
		since, app, code).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("contando erros: %w", err)
	}
	return int(count), nil //nolint:gosec
}

// FindReleases lista releases ativas na janela com contadores agregados.
// Ignora eventos sem release (SDK antigo ou app que não configurou).
func (r *Repository) FindReleases(ctx context.Context, f domain.ReleaseFilter) ([]domain.Release, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	from := time.Now().Add(-7 * 24 * time.Hour).UTC()
	to := time.Now().UTC()
	if f.From != nil {
		from = *f.From
	}
	if f.To != nil {
		to = *f.To
	}

	// AppScope entra como literal IN(...) — o driver não aceita array em placeholder.
	scopeCond := ""
	if len(f.AppScope) > 0 {
		quoted := make([]string, len(f.AppScope))
		for i, v := range f.AppScope {
			quoted[i] = "'" + strings.ReplaceAll(v, "'", "''") + "'"
		}
		scopeCond = " AND app IN (" + strings.Join(quoted, ", ") + ")"
	}

	query := fmt.Sprintf(`
		SELECT release,
		       any(app) AS app_agg,
		       min(occurred_at) AS first_seen,
		       max(occurred_at) AS last_seen,
		       toUInt64(count()) AS events,
		       toUInt64(countIf%s) AS errors,
		       toUInt64(uniqExact(session_id)) AS sessions,
		       toUInt64(uniqExactIf(user_id, user_id != '')) AS users,
		       coalesce(avg(duration_ms), 0) AS avg_ms
		FROM trace_events FINAL
		WHERE occurred_at >= $1 AND occurred_at < $2
		  AND release != ''
		  AND ($3 = '' OR app = $3)%s
		GROUP BY release
		ORDER BY last_seen DESC
		LIMIT %d`, errorCond, scopeCond, f.Limit)

	rows, err := r.conn.Query(ctx, query, from, to, f.App)
	if err != nil {
		return nil, fmt.Errorf("listando releases: %w", err)
	}
	defer rows.Close()

	out := []domain.Release{}
	for rows.Next() {
		var rel domain.Release
		var events, errors, sessions, users uint64
		if err := rows.Scan(&rel.Release, &rel.App, &rel.FirstSeen, &rel.LastSeen,
			&events, &errors, &sessions, &users, &rel.AvgDurationMs); err != nil {
			return nil, fmt.Errorf("lendo release: %w", err)
		}
		rel.Events = int(events)     //nolint:gosec
		rel.Errors = int(errors)     //nolint:gosec
		rel.Sessions = int(sessions) //nolint:gosec
		rel.Users = int(users)       //nolint:gosec
		if rel.Events > 0 {
			rel.ErrorRate = float64(rel.Errors) / float64(rel.Events)
		}
		rel.FirstSeen = rel.FirstSeen.UTC()
		rel.LastSeen = rel.LastSeen.UTC()
		out = append(out, rel)
	}
	return out, rows.Err()
}

// GetRelease retorna os agregados de uma release específica na janela — usado
// pelo endpoint de comparação (releaseA vs releaseB).
func (r *Repository) GetRelease(ctx context.Context, app, release string, from, to time.Time) (domain.Release, error) {
	var rel domain.Release
	var events, errors, sessions, users uint64
	err := r.conn.QueryRow(ctx, fmt.Sprintf(`
		SELECT any(app) AS app_agg,
		       min(occurred_at) AS first_seen,
		       max(occurred_at) AS last_seen,
		       toUInt64(count()) AS events,
		       toUInt64(countIf%s) AS errors,
		       toUInt64(uniqExact(session_id)) AS sessions,
		       toUInt64(uniqExactIf(user_id, user_id != '')) AS users,
		       coalesce(avg(duration_ms), 0) AS avg_ms
		FROM trace_events FINAL
		WHERE occurred_at >= $1 AND occurred_at < $2
		  AND ($3 = '' OR app = $3)
		  AND release = $4`, errorCond),
		from, to, app, release).
		Scan(&rel.App, &rel.FirstSeen, &rel.LastSeen, &events, &errors, &sessions, &users, &rel.AvgDurationMs)
	if err != nil {
		return domain.Release{}, fmt.Errorf("consultando release: %w", err)
	}
	rel.Release = release
	rel.Events = int(events)     //nolint:gosec
	rel.Errors = int(errors)     //nolint:gosec
	rel.Sessions = int(sessions) //nolint:gosec
	rel.Users = int(users)       //nolint:gosec
	if rel.Events > 0 {
		rel.ErrorRate = float64(rel.Errors) / float64(rel.Events)
	}
	rel.FirstSeen = rel.FirstSeen.UTC()
	rel.LastSeen = rel.LastSeen.UTC()
	return rel, nil
}

// RunFunnel executa um funil via windowFunnel: para cada sessão, quantas
// condições consecutivas foram satisfeitas dentro da janela. Depois conta
// quantas sessões chegaram em cada nível.
//
// A construção do SQL é dinâmica (N steps → N argumentos para windowFunnel);
// cada argumento é uma condição composta com AND. Usamos named params para
// não misturar com placeholders posicionais.
func (r *Repository) RunFunnel(ctx context.Context, f domain.FunnelRun) (domain.FunnelResult, error) {
	if len(f.Steps) < 2 {
		return domain.FunnelResult{}, fmt.Errorf("funnel precisa de pelo menos 2 steps")
	}
	if f.WindowSeconds <= 0 {
		f.WindowSeconds = 1800
	}
	from := time.Now().Add(-7 * 24 * time.Hour).UTC()
	to := time.Now().UTC()
	if f.From != nil {
		from = *f.From
	}
	if f.To != nil {
		to = *f.To
	}

	// Monta as condições dos steps. Cada step vira uma expressão booleana.
	// Os valores literais são escapados com aspas simples e nomes de campo
	// vêm de uma whitelist (só as chaves da struct) — não há injeção possível.
	conds := make([]string, len(f.Steps))
	for i, s := range f.Steps {
		parts := []string{fmt.Sprintf("event_type = %s", quote(s.Match.Type))}
		if s.Match.Name != "" {
			parts = append(parts, fmt.Sprintf("name = %s", quote(s.Match.Name)))
		}
		if s.Match.Screen != "" {
			parts = append(parts, fmt.Sprintf("screen = %s", quote(s.Match.Screen)))
		}
		if s.Match.Feature != "" {
			parts = append(parts, fmt.Sprintf("feature = %s", quote(s.Match.Feature)))
		}
		if s.Match.HTTPURL != "" {
			parts = append(parts, fmt.Sprintf("positionCaseInsensitive(http_url, %s) > 0", quote(s.Match.HTTPURL)))
		}
		conds[i] = "(" + strings.Join(parts, " AND ") + ")"
	}

	// windowFunnel exige DateTime (segundo), não DateTime64. Convertemos com
	// toDateTime — perdemos precisão sub-segundo, o que não importa para funil.
	query := fmt.Sprintf(`
		SELECT level, toUInt64(count()) AS c FROM (
			SELECT session_id, windowFunnel(%d)(toDateTime(occurred_at), %s) AS level
			FROM trace_events FINAL
			WHERE occurred_at >= @from AND occurred_at < @to
			  AND (@app = '' OR app = @app)
			GROUP BY session_id
		)
		GROUP BY level ORDER BY level`, f.WindowSeconds, strings.Join(conds, ", "))

	rows, err := r.conn.Query(ctx, query,
		clickhouse.Named("from", from),
		clickhouse.Named("to", to),
		clickhouse.Named("app", f.App))
	if err != nil {
		return domain.FunnelResult{}, fmt.Errorf("executando funnel: %w", err)
	}
	defer rows.Close()

	// levelCounts[i] = sessões que atingiram level exatamente i (0..N).
	levelCounts := make([]int, len(f.Steps)+1)
	for rows.Next() {
		var level uint8
		var c uint64
		if err := rows.Scan(&level, &c); err != nil {
			return domain.FunnelResult{}, err
		}
		if int(level) < len(levelCounts) {
			levelCounts[level] = int(c) //nolint:gosec
		}
	}

	// Sessões que atingiram step i = soma dos que chegaram em i, i+1, ..., N.
	atStep := make([]int, len(f.Steps))
	sum := 0
	for i := len(f.Steps); i > 0; i-- {
		sum += levelCounts[i]
		atStep[i-1] = sum
	}

	total := atStep[0]
	result := domain.FunnelResult{TotalSessions: total, Steps: make([]domain.FunnelStepResult, len(f.Steps))}
	for i, step := range f.Steps {
		s := domain.FunnelStepResult{Name: step.Name, Sessions: atStep[i]}
		if total > 0 {
			s.OverallRate = float64(atStep[i]) / float64(total)
		}
		if i == 0 {
			s.StepConversion = 1
		} else if atStep[i-1] > 0 {
			s.StepConversion = float64(atStep[i]) / float64(atStep[i-1])
			s.DropoffFromPrev = atStep[i-1] - atStep[i]
		}
		result.Steps[i] = s
	}
	return result, nil
}

// Retention monta uma matriz cohort × Dn. Cohort = users que fizeram o
// primeiro evento naquele período (default week). Retido em Dn = user que
// voltou entre [start_of_cohort + N dias, +N+1 dias).
func (r *Repository) Retention(ctx context.Context, f domain.RetentionFilter) (domain.RetentionResult, error) {
	from := time.Now().Add(-30 * 24 * time.Hour).UTC()
	to := time.Now().UTC()
	if f.From != nil {
		from = *f.From
	}
	if f.To != nil {
		to = *f.To
	}
	cohortFn := "toStartOfWeek"
	if f.CohortBy == "day" {
		cohortFn = "toStartOfDay"
	} else if f.CohortBy == "month" {
		cohortFn = "toStartOfMonth"
	}
	offsets := f.OffsetsDays
	if len(offsets) == 0 {
		offsets = []int{1, 7, 14, 30}
	}

	// Estratégia sem JOIN com range (ClickHouse rejeita >= no ON):
	// GROUP BY user_id retorna first_seen + array de occurred_at.
	// Para cada offset, `arrayExists` testa se algum evento caiu na janela
	// [first_seen + N dias, first_seen + N+1 dias).
	sums := make([]string, len(offsets))
	for i, off := range offsets {
		sums[i] = fmt.Sprintf(
			`toUInt64(countIf(arrayExists(x -> x >= first_seen + INTERVAL %d DAY AND x < first_seen + INTERVAL %d DAY, seen_ats))) AS d%d`,
			off, off+1, off)
	}

	query := fmt.Sprintf(`
		WITH firsts AS (
			SELECT user_id,
			       min(occurred_at) AS first_seen,
			       groupArray(occurred_at) AS seen_ats
			FROM trace_events FINAL
			WHERE user_id != '' AND occurred_at >= @from AND occurred_at < @to
			  AND (@app = '' OR app = @app)
			GROUP BY user_id
		)
		SELECT %s(first_seen) AS cohort,
		       toUInt64(uniqExact(user_id)) AS new_users,
		       %s
		FROM firsts
		GROUP BY cohort
		ORDER BY cohort`, cohortFn, strings.Join(sums, ", "))

	rows, err := r.conn.Query(ctx, query,
		clickhouse.Named("from", from),
		clickhouse.Named("to", to),
		clickhouse.Named("app", f.App))
	if err != nil {
		return domain.RetentionResult{}, fmt.Errorf("cohort retention: %w", err)
	}
	defer rows.Close()

	out := domain.RetentionResult{
		CohortBy:    f.CohortBy,
		OffsetsDays: offsets,
		Cohorts:     []domain.RetentionCohort{}, // sem dados → array vazio, nunca null no JSON
	}
	if out.CohortBy == "" {
		out.CohortBy = "week"
	}
	for rows.Next() {
		// Scan dinâmico: cohort + new_users + N offsets.
		values := make([]any, 2+len(offsets))
		var cohort time.Time
		var newUsers uint64
		counters := make([]uint64, len(offsets))
		values[0], values[1] = &cohort, &newUsers
		for i := range counters {
			values[2+i] = &counters[i]
		}
		if err := rows.Scan(values...); err != nil {
			return domain.RetentionResult{}, err
		}
		c := domain.RetentionCohort{
			Cohort:      cohort.UTC(),
			NewUsers:    int(newUsers), //nolint:gosec
			Retained:    map[string]int{},
			RetainedPct: map[string]float64{},
		}
		for i, off := range offsets {
			key := fmt.Sprintf("D%d", off)
			n := int(counters[i]) //nolint:gosec
			c.Retained[key] = n
			if c.NewUsers > 0 {
				c.RetainedPct[key] = float64(n) / float64(c.NewUsers)
			}
		}
		out.Cohorts = append(out.Cohorts, c)
	}
	return out, rows.Err()
}

// quote escapa uma string para embutir com segurança em SQL literal —
// duplica aspas simples. Só é usado internamente com valores validados
// (nomes de campo vêm de whitelist).
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// FindWebVitals agrega Web Vitals (LCP, CLS, INP, FID, TTFB, FCP) por rota.
// Os vitals chegam como eventos custom com nome `web_vital_<lower>` e valor
// em metadata.value/metadata.vital/metadata.screen — enviados pelo SDK.
func (r *Repository) FindWebVitals(ctx context.Context, f domain.WebVitalFilter) ([]domain.WebVitalStat, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	from := time.Now().Add(-24 * time.Hour).UTC()
	to := time.Now().UTC()
	if f.From != nil {
		from = *f.From
	}
	if f.To != nil {
		to = *f.To
	}

	// Extrai value/vital/rating do JSON de metadata (JSONExtract*).
	// Se o vital foi filtrado, restringe o LIKE no nome do evento.
	nameFilter := "name LIKE 'web_vital_%'"
	if f.Vital != "" {
		nameFilter = fmt.Sprintf("name = 'web_vital_%s'", strings.ToLower(f.Vital))
	}

	scopeCond := ""
	if len(f.AppScope) > 0 {
		quoted := make([]string, len(f.AppScope))
		for i, v := range f.AppScope {
			quoted[i] = "'" + strings.ReplaceAll(v, "'", "''") + "'"
		}
		scopeCond = " AND app IN (" + strings.Join(quoted, ", ") + ")"
	}

	query := fmt.Sprintf(`
		SELECT
			upper(JSONExtractString(metadata, 'vital')) AS vital,
			coalesce(nullIf(JSONExtractString(metadata, 'screen'), ''), screen) AS route,
			toUInt64(count()) AS c,
			quantile(0.75)(JSONExtractFloat(metadata, 'value')) AS p75,
			quantile(0.95)(JSONExtractFloat(metadata, 'value')) AS p95,
			toUInt64(countIf(JSONExtractString(metadata, 'rating') = 'good')) AS good,
			toUInt64(countIf(JSONExtractString(metadata, 'rating') = 'poor')) AS poor
		FROM trace_events FINAL
		WHERE event_type = 'custom' AND %s
		  AND occurred_at >= $1 AND occurred_at < $2
		  AND ($3 = '' OR app = $3)%s
		GROUP BY vital, route
		HAVING vital != ''
		ORDER BY vital, c DESC
		LIMIT %d`, nameFilter, scopeCond, f.Limit)

	rows, err := r.conn.Query(ctx, query, from, to, f.App)
	if err != nil {
		return nil, fmt.Errorf("agregando web vitals: %w", err)
	}
	defer rows.Close()

	out := []domain.WebVitalStat{}
	for rows.Next() {
		var v domain.WebVitalStat
		var c, good, poor uint64
		if err := rows.Scan(&v.Vital, &v.Screen, &c, &v.P75, &v.P95, &good, &poor); err != nil {
			return nil, fmt.Errorf("lendo web vital: %w", err)
		}
		v.Count, v.Good, v.Poor = int(c), int(good), int(poor) //nolint:gosec
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetFilterOptions retorna os valores distintos para os dropdowns do dashboard.
func (r *Repository) GetFilterOptions(ctx context.Context) (domain.FilterOptions, error) {
	opts := domain.FilterOptions{Apps: []string{}, Types: []string{}, Features: []string{}, Names: []string{}}

	collect := func(query string, dest *[]string) error {
		rows, err := r.conn.Query(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				return err
			}
			*dest = append(*dest, v)
		}
		return rows.Err()
	}

	if err := collect(`SELECT DISTINCT app FROM trace_events ORDER BY app`, &opts.Apps); err != nil {
		return opts, fmt.Errorf("apps: %w", err)
	}
	if err := collect(`SELECT DISTINCT event_type FROM trace_events ORDER BY event_type`, &opts.Types); err != nil {
		return opts, fmt.Errorf("types: %w", err)
	}
	if err := collect(`SELECT DISTINCT feature FROM trace_events WHERE feature != '' ORDER BY feature LIMIT 200`, &opts.Features); err != nil {
		return opts, fmt.Errorf("features: %w", err)
	}
	if err := collect(`SELECT DISTINCT name FROM trace_events ORDER BY name LIMIT 500`, &opts.Names); err != nil {
		return opts, fmt.Errorf("names: %w", err)
	}
	return opts, nil
}

// ---------------------------------------------------------------------------
// Scan helpers
// ---------------------------------------------------------------------------

func scanEvents(rows driver.Rows) ([]domain.TraceEvent, error) {
	events := []domain.TraceEvent{}
	for rows.Next() {
		var e domain.TraceEvent
		var typ, method, url, reqBody, respBody, errCode, errMsg, errBody, metadata string
		var traceID, spanID, parentSpanID string
		var status *uint16
		var duration *int32

		if err := rows.Scan(&e.ID, &e.SessionID, &e.UserID, &e.App, &e.Release, &typ, &e.Name,
			&e.Feature, &e.Screen,
			&method, &url, &status, &duration,
			&reqBody, &respBody, &errCode, &errMsg, &errBody,
			&metadata, &e.OccurredAt, &e.ReceivedAt,
			&traceID, &spanID, &parentSpanID); err != nil {
			return nil, fmt.Errorf("lendo evento: %w", err)
		}
		if traceID != "" || spanID != "" {
			e.Trace = &domain.TraceContext{TraceID: traceID, SpanID: spanID, ParentSpanID: parentSpanID}
		}
		e.Type = domain.EventType(typ)
		if duration != nil {
			d := int(*duration)
			e.DurationMs = &d
		}
		e.Metadata = jsonOrNil(metadata)
		if method != "" || url != "" || status != nil || reqBody != "" || respBody != "" {
			var statusInt *int
			if status != nil {
				s := int(*status)
				statusInt = &s
			}
			e.HTTP = &domain.HTTPInfo{
				Method:       method,
				URL:          url,
				StatusCode:   statusInt,
				RequestBody:  jsonOrNil(reqBody),
				ResponseBody: jsonOrNil(respBody),
			}
		}
		if errCode != "" || errMsg != "" || errBody != "" {
			e.Error = &domain.ErrorInfo{Code: errCode, Message: errMsg, Body: jsonOrNil(errBody)}
		}
		e.OccurredAt = e.OccurredAt.UTC()
		e.ReceivedAt = e.ReceivedAt.UTC()
		events = append(events, e)
	}
	return events, rows.Err()
}

func scanSession(rows driver.Rows, withTotal bool) (domain.Session, int, error) {
	var s domain.Session
	var attrs string
	var eventCount, errorCount, total uint64
	dest := []any{&s.SessionID, &s.UserID, &s.App, &s.UserAgent, &attrs,
		&s.StartedAt, &s.LastEventAt, &eventCount, &errorCount}
	if withTotal {
		dest = append(dest, &total)
	}
	if err := rows.Scan(dest...); err != nil {
		return s, 0, fmt.Errorf("lendo sessão: %w", err)
	}
	s.Attributes = jsonOrNil(attrs)
	s.EventCount = int(eventCount) //nolint:gosec
	s.ErrorCount = int(errorCount) //nolint:gosec
	s.StartedAt = s.StartedAt.UTC()
	s.LastEventAt = s.LastEventAt.UTC()
	return s, int(total), nil //nolint:gosec
}

func jsonOrNil(s string) json.RawMessage {
	if s == "" {
		return nil
	}
	return json.RawMessage(s)
}

func rawOrEmptyObject(b json.RawMessage) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage(`{}`)
	}
	return b
}

// EncodeCursor serializa o cursor keyset em base64 url-safe.
func EncodeCursor(t time.Time, id string) string {
	raw, _ := json.Marshal(map[string]string{"t": t.Format(time.RFC3339Nano), "id": id})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeCursor reverte EncodeCursor.
func DecodeCursor(s string) (*domain.Cursor, error) {
	if s == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, domain.NewValidationError("cursor inválido")
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, domain.NewValidationError("cursor inválido")
	}
	t, err := time.Parse(time.RFC3339Nano, m["t"])
	if err != nil {
		return nil, domain.NewValidationError("cursor inválido")
	}
	return &domain.Cursor{OccurredAt: t, ID: m["id"]}, nil
}

func adaptiveBucket(from, to time.Time) time.Duration {
	window := to.Sub(from)
	switch {
	case window <= 2*time.Hour:
		return 2 * time.Minute
	case window <= 6*time.Hour:
		return 5 * time.Minute
	case window <= 24*time.Hour:
		return 30 * time.Minute
	case window <= 7*24*time.Hour:
		return 3 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// ---------------------------------------------------------------------------
// queryBuilder — SQL dinâmico sempre parametrizado (placeholders posicionais)
// ---------------------------------------------------------------------------

type queryBuilder struct {
	base    string
	conds   []string
	havings []string
	group   string
	args    []any
	order   string
	lim     int
	hasLim  bool
}

func newQueryBuilder(base string) *queryBuilder {
	return &queryBuilder{base: base}
}

func (q *queryBuilder) arg(v any) string {
	q.args = append(q.args, v)
	return fmt.Sprintf("$%d", len(q.args))
}

func (q *queryBuilder) where(cond string) {
	q.conds = append(q.conds, cond)
}

func (q *queryBuilder) whereEq(col, val string) {
	if val != "" {
		q.conds = append(q.conds, col+" = "+q.arg(val))
	}
}

// whereInStr adiciona `col IN (v1, v2, ...)` — usado para restringir a lista
// de apps que o usuário autenticado pode ver (multi-tenancy). Lista vazia é
// no-op (super-admin, sem escopo). Valores são escapados manualmente porque
// o driver não aceita array como placeholder posicional.
func (q *queryBuilder) whereInStr(col string, vals []string) {
	if len(vals) == 0 {
		return
	}
	quoted := make([]string, len(vals))
	for i, v := range vals {
		quoted[i] = "'" + strings.ReplaceAll(v, "'", "''") + "'"
	}
	q.conds = append(q.conds, col+" IN ("+strings.Join(quoted, ", ")+")")
}

func (q *queryBuilder) whereTime(col string, from, to *time.Time) {
	if from != nil {
		q.conds = append(q.conds, col+" >= "+q.arg(*from))
	}
	if to != nil {
		q.conds = append(q.conds, col+" < "+q.arg(*to))
	}
}

func (q *queryBuilder) having(cond string)  { q.havings = append(q.havings, cond) }
func (q *queryBuilder) groupBy(cols string) { q.group = cols }
func (q *queryBuilder) orderBy(o string)    { q.order = o }
func (q *queryBuilder) limit(n int)         { q.lim, q.hasLim = n, true }

func (q *queryBuilder) sql() string {
	var sb strings.Builder
	sb.WriteString(q.base)
	if len(q.conds) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(q.conds, " AND "))
	}
	if q.group != "" {
		sb.WriteString(" GROUP BY ")
		sb.WriteString(q.group)
	}
	if len(q.havings) > 0 {
		sb.WriteString(" HAVING ")
		sb.WriteString(strings.Join(q.havings, " AND "))
	}
	if q.order != "" {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(q.order)
	}
	if q.hasLim {
		fmt.Fprintf(&sb, " LIMIT %d", q.lim)
	}
	return sb.String()
}
