'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { Button, Checkbox, Field, Input } from '@/components/form';
import { Modal, ModalActions } from '@/components/Modal';
import { api, type SavedView } from '@/lib/api';
import { canShareViews, getSessionUser } from '@/lib/auth';

/**
 * Menu de views salvas para uma tela. Recebe:
 *  - viewType: chave do tipo (ex.: 'traces') — segrega no backend
 *  - currentFilters: snapshot do que aplicar quando salvar
 *  - onApply: como aplicar filters ao carregar uma view (a tela decide como)
 *
 * Uso típico na barra de filtros:
 *   <SavedViewsMenu viewType="traces"
 *                   currentFilters={fromURL()}
 *                   onApply={(f) => applyToURL(f)} />
 */
export function SavedViewsMenu({
  viewType,
  currentFilters,
  onApply,
}: {
  viewType: string;
  currentFilters: Record<string, unknown>;
  onApply: (filters: Record<string, unknown>) => void;
}) {
  const qc = useQueryClient();
  const me = getSessionUser();
  const [open, setOpen] = useState(false);
  const [saveOpen, setSaveOpen] = useState(false);
  const [saveName, setSaveName] = useState('');
  const [saveShared, setSaveShared] = useState(false);
  const [feedback, setFeedback] = useState('');
  const ref = useRef<HTMLDivElement>(null);

  const { data } = useQuery({
    queryKey: ['saved-views', viewType],
    queryFn: () => api.listSavedViews(viewType),
  });

  useEffect(() => {
    if (!open) return;
    const onClick = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', onClick);
    return () => document.removeEventListener('mousedown', onClick);
  }, [open]);

  const invalidate = () => qc.invalidateQueries({ queryKey: ['saved-views', viewType] });

  const create = useMutation({
    mutationFn: () =>
      api.createSavedView({
        name: saveName,
        viewType,
        filters: currentFilters,
        isShared: saveShared,
      }),
    onSuccess: () => {
      setSaveOpen(false);
      setSaveName('');
      setSaveShared(false);
      invalidate();
    },
    onError: (err: unknown) => {
      setFeedback(
        err instanceof Error ? err.message : 'Erro ao salvar (nome duplicado?)',
      );
    },
  });

  const remove = useMutation({
    mutationFn: (id: string) => api.deleteSavedView(id),
    onSuccess: () => invalidate(),
  });

  const mine = data?.views.filter((v) => v.ownerUserId === me?.id) ?? [];
  const shared = data?.views.filter((v) => v.ownerUserId !== me?.id) ?? [];

  return (
    <>
      <div ref={ref} className="relative">
        <Button icon="★" onClick={() => setOpen((v) => !v)}>
          Views
          {data && data.views.length > 0 ? (
            <span className="ml-1 rounded bg-plane px-1.5 text-[10px] text-muted">
              {data.views.length}
            </span>
          ) : null}
        </Button>
        {open ? (
          <div className="absolute right-0 z-30 mt-1 min-w-[280px] rounded-md border border-hairline bg-surface p-1 shadow-xl">
            <button
              type="button"
              onClick={() => {
                setOpen(false);
                setSaveOpen(true);
              }}
              className="flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm text-accent hover:bg-plane"
            >
              + Salvar filtros atuais como view
            </button>
            {mine.length > 0 ? (
              <ViewSection
                title="Minhas views"
                items={mine}
                onApply={(v) => {
                  setOpen(false);
                  onApply(v.filters);
                }}
                onRemove={(id) => {
                  if (confirm('Remover esta view?')) remove.mutate(id);
                }}
                canRemove={() => true}
              />
            ) : null}
            {shared.length > 0 ? (
              <ViewSection
                title="Compartilhadas pelo time"
                items={shared}
                onApply={(v) => {
                  setOpen(false);
                  onApply(v.filters);
                }}
                onRemove={() => {
                  /* não posso remover a de outro */
                }}
                canRemove={() => false}
              />
            ) : null}
            {mine.length === 0 && shared.length === 0 ? (
              <p className="px-3 py-2 text-xs text-muted">
                Ainda não há views salvas — comece por “salvar filtros atuais”.
              </p>
            ) : null}
          </div>
        ) : null}
      </div>

      <Modal
        open={saveOpen}
        onClose={() => setSaveOpen(false)}
        title="Salvar view"
        description="Guarda os filtros atuais com um nome. Pode ser privada ou compartilhada com o time."
        size="sm"
      >
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            if (saveName.trim()) create.mutate();
          }}
        >
          <Field label="Nome" required>
            <Input
              autoFocus
              required
              placeholder="ex.: Erros 5xx do portal"
              value={saveName}
              onChange={(e) => setSaveName(e.target.value)}
            />
          </Field>
          {canShareViews(me) ? (
            <Checkbox
              checked={saveShared}
              onChange={(e) => setSaveShared(e.target.checked)}
              label="compartilhar com o time (aparece pra todos)"
            />
          ) : (
            <p className="text-xs text-muted">
              Sua view fica privada. Editores e admins podem compartilhar com o time.
            </p>
          )}
          {feedback ? <p className="text-sm text-critical">{feedback}</p> : null}
          <ModalActions>
            <Button onClick={() => setSaveOpen(false)}>cancelar</Button>
            <Button variant="primary" type="submit" loading={create.isPending}>
              salvar
            </Button>
          </ModalActions>
        </form>
      </Modal>
    </>
  );
}

function ViewSection({
  title,
  items,
  onApply,
  onRemove,
  canRemove,
}: {
  title: string;
  items: SavedView[];
  onApply: (v: SavedView) => void;
  onRemove: (id: string) => void;
  canRemove: (v: SavedView) => boolean;
}) {
  return (
    <div className="mt-1 border-t border-hairline pt-1">
      <p className="px-3 py-1 text-[10px] font-semibold uppercase tracking-wider text-muted">
        {title}
      </p>
      <ul>
        {items.map((v) => (
          <li key={v.id} className="group flex items-center gap-1 px-2">
            <button
              type="button"
              onClick={() => onApply(v)}
              className="flex-1 truncate rounded-md px-1.5 py-1.5 text-left text-sm text-ink hover:bg-plane"
              title={v.ownerName ? `por ${v.ownerName}` : undefined}
            >
              {v.name}
              {v.isShared ? (
                <span className="ml-1 text-[10px] text-muted">· compartilhada</span>
              ) : null}
            </button>
            {canRemove(v) ? (
              <button
                type="button"
                onClick={() => onRemove(v.id)}
                aria-label="Remover"
                className="rounded p-1 text-xs text-muted opacity-0 hover:text-critical group-hover:opacity-100"
              >
                ✕
              </button>
            ) : null}
          </li>
        ))}
      </ul>
    </div>
  );
}
