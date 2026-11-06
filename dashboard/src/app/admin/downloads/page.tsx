'use client';

import { useMutation, useQuery } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { Button, Field, Textarea } from '@/components/form';
import { EmptyState, ErrorState } from '@/components/ui';
import { api, ApiError, type DownloadJob } from '@/lib/api';
import { fmtDateTime } from '@/lib/time';

const STATUS_LABEL: Record<DownloadJob['status'], string> = {
  queued: 'Na fila',
  running: 'Processando',
  done: 'Pronto',
  error: 'Erro',
};

const STATUS_STYLE: Record<DownloadJob['status'], string> = {
  queued: 'bg-plane text-muted',
  running: 'bg-accent/10 text-accent',
  done: 'bg-good/10 text-good',
  error: 'bg-critical/10 text-critical',
};

function parseUrls(raw: string): string[] {
  return raw
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
}

function fmtBytes(n?: number): string {
  if (!n) return '—';
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

const LAST_JOB_KEY = 'ndovu.downloads.lastJob';

export default function DownloadsPage() {
  const [raw, setRaw] = useState('');
  const [jobId, setJobId] = useState<string | null>(null);
  const [feedback, setFeedback] = useState('');

  // Recupera o último job após refresh (o job vive no servidor; a UI só
  // guarda o id). Feito no efeito para não divergir na hidratação SSR.
  useEffect(() => {
    const saved = window.localStorage.getItem(LAST_JOB_KEY);
    if (saved) setJobId(saved);
  }, []);

  function rememberJob(id: string | null) {
    setJobId(id);
    if (id) window.localStorage.setItem(LAST_JOB_KEY, id);
    else window.localStorage.removeItem(LAST_JOB_KEY);
  }

  const create = useMutation({
    mutationFn: () => api.createDownload(parseUrls(raw)),
    onSuccess: (job) => {
      rememberJob(job.id);
      setFeedback('');
    },
    onError: (err) =>
      setFeedback(err instanceof ApiError ? (err.details?.join('; ') ?? err.message) : 'Erro ao criar job'),
  });

  const {
    data: job,
    error,
  } = useQuery({
    queryKey: ['download-job', jobId],
    queryFn: () => api.getDownload(jobId as string),
    enabled: !!jobId,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === 'done' || status === 'error' ? false : 2000;
    },
  });

  const urls = parseUrls(raw);
  const progress = job && job.total > 0 ? Math.round((job.done / job.total) * 100) : 0;

  return (
    <div className="flex flex-col gap-6">
      <header>
        <h1 className="text-xl font-semibold text-ink">Downloads MP3</h1>
        <p className="mt-1 text-sm text-muted">
          Cole links de vídeos do YouTube, o serviço converte o áudio com yt-dlp + ffmpeg e
          entrega um .zip. Ferramenta restrita a admin; o zip fica disponível por 24h.
        </p>
      </header>

      <div className="rounded-lg border border-hairline bg-surface p-4">
        <Field
          label="Links do YouTube (um por linha)"
          hint={`${urls.length} link(s) — máximo de 10 por job`}
        >
          <Textarea
            rows={6}
            value={raw}
            onChange={(e) => setRaw(e.target.value)}
            placeholder={'https://www.youtube.com/watch?v=...\nhttps://youtu.be/...'}
          />
        </Field>

        <div className="mt-3 flex items-center gap-3">
          <Button
            variant="primary"
            loading={create.isPending}
            disabled={urls.length === 0}
            onClick={() => create.mutate()}
          >
            Criar job
          </Button>
          {jobId ? (
            <Button
              variant="ghost"
              onClick={() => {
                rememberJob(null);
                setRaw('');
                setFeedback('');
              }}
            >
              Novo job
            </Button>
          ) : null}
          {feedback ? <span className="text-sm text-critical">{feedback}</span> : null}
        </div>

        <p className="mt-3 text-xs text-muted">
          Use apenas conteúdo próprio ou com licença que permita download. Baixar vídeos de
          terceiros viola os Termos de Serviço do YouTube.
        </p>
      </div>

      {jobId ? (
        <div className="rounded-lg border border-hairline bg-surface p-4">
          {error ? (
            <ErrorState message={error instanceof Error ? error.message : 'Erro ao consultar o job'} />
          ) : !job ? (
            <p className="text-sm text-muted">Consultando job…</p>
          ) : (
            <div className="flex flex-col gap-4">
              <div className="flex flex-wrap items-center gap-3">
                <span className={`rounded-full px-2.5 py-0.5 text-xs font-medium ${STATUS_STYLE[job.status]}`}>
                  {STATUS_LABEL[job.status]}
                </span>
                <span className="font-mono text-xs text-muted">{job.id}</span>
                <span className="text-xs text-muted">criado em {fmtDateTime(job.createdAt)}</span>
              </div>

              <div>
                <div className="mb-1 flex justify-between text-xs text-muted">
                  <span>
                    {job.done} de {job.total} vídeo(s)
                  </span>
                  <span>{progress}%</span>
                </div>
                <div className="h-2 overflow-hidden rounded-full bg-plane">
                  <div
                    className="h-full rounded-full bg-accent transition-all"
                    style={{ width: `${progress}%` }}
                  />
                </div>
              </div>

              {job.status === 'done' ? (
                <div className="flex flex-wrap items-center gap-3">
                  <a
                    href={api.downloadArchiveUrl(job.id)}
                    className="inline-flex h-9 items-center rounded-md bg-accent px-3.5 text-sm font-medium text-white hover:opacity-90"
                  >
                    Baixar .zip ({fmtBytes(job.archiveBytes)})
                  </a>
                  {job.failed?.length ? (
                    <span className="text-xs text-warn">
                      {job.failed.length} vídeo(s) falharam — veja falhas.txt no zip
                    </span>
                  ) : null}
                </div>
              ) : null}

              {job.status === 'error' ? <ErrorState message={job.error ?? 'Job falhou'} /> : null}

              {job.failed?.length ? (
                <div>
                  <h2 className="mb-2 text-sm font-medium text-ink-2">Falhas</h2>
                  <ul className="flex flex-col gap-1 text-xs">
                    {job.failed.map((f) => (
                      <li key={f.url} className="rounded border border-hairline bg-plane px-2 py-1">
                        <span className="block truncate font-mono text-ink-2">{f.url}</span>
                        <span className="text-critical">{f.error}</span>
                      </li>
                    ))}
                  </ul>
                </div>
              ) : null}
            </div>
          )}
        </div>
      ) : (
        <EmptyState message="Nenhum job nesta sessão. Cole links acima para começar." />
      )}
    </div>
  );
}
