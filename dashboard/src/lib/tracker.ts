import type { Issue } from './types';

/**
 * Helpers para "exportar" um issue do Ndovu para um issue tracker externo.
 * Não fazemos integração via API (evita OAuth por enquanto) — geramos uma
 * URL que abre o formulário de criação já com título + descrição preenchidos.
 *
 * Config por env (público, entra no bundle):
 *   NEXT_PUBLIC_JIRA_BASE   → ex.: https://acme.atlassian.net (opcional)
 *   NEXT_PUBLIC_JIRA_PROJECT → chave do projeto (ex.: WEB)
 *   NEXT_PUBLIC_LINEAR_TEAM  → key do team no Linear (ex.: eng)
 */

const JIRA_BASE = process.env.NEXT_PUBLIC_JIRA_BASE ?? '';
const JIRA_PROJECT = process.env.NEXT_PUBLIC_JIRA_PROJECT ?? '';
const LINEAR_TEAM = process.env.NEXT_PUBLIC_LINEAR_TEAM ?? '';

function titleFor(issue: Issue): string {
  const code = issue.code ? `[${issue.code}] ` : '';
  return `${code}${issue.message || issue.name}`.slice(0, 200);
}

function descriptionFor(issue: Issue, ndovuUrl: string): string {
  const lines = [
    `**App:** ${issue.app}`,
    `**Ocorrências:** ${issue.count} · **Usuários afetados:** ${issue.affectedUsers}`,
    `**Primeira vez:** ${issue.firstSeen}`,
    `**Última vez:** ${issue.lastSeen}`,
    '',
    `**Mensagem:** ${issue.message || '—'}`,
    issue.sampleUrl ? `**Rota exemplo:** \`${issue.sampleUrl}\`` : '',
    '',
    `Referência no Ndovu: ${ndovuUrl}`,
    `Fingerprint: \`${issue.fingerprint}\``,
  ];
  return lines.filter(Boolean).join('\n');
}

export function jiraUrl(issue: Issue, ndovuUrl: string): string | null {
  if (!JIRA_BASE) return null;
  const summary = titleFor(issue);
  const description = descriptionFor(issue, ndovuUrl);
  const params = new URLSearchParams({
    pid: JIRA_PROJECT,
    issuetype: '10004', // Bug
    summary,
    description,
  });
  return `${JIRA_BASE.replace(/\/$/, '')}/secure/CreateIssueDetails!init.jspa?${params.toString()}`;
}

export function linearUrl(issue: Issue, ndovuUrl: string): string | null {
  const team = LINEAR_TEAM;
  if (!team) return null;
  const title = titleFor(issue);
  const description = descriptionFor(issue, ndovuUrl);
  const params = new URLSearchParams({ title, description });
  return `https://linear.app/${encodeURIComponent(team)}/new?${params.toString()}`;
}
