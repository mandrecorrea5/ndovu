'use client';

import { useQuery } from '@tanstack/react-query';
import { useRouter, useSearchParams } from 'next/navigation';
import { useCallback, useEffect, useState } from 'react';
import { Button, Checkbox, Field, Input, Select } from '@/components/form';
import { SavedViewsMenu } from '@/components/SavedViewsMenu';
import { api } from '@/lib/api';
import { RANGE_PRESETS } from '@/lib/time';

// Chaves de filtro que fazem sentido persistir numa view salva.
const VIEW_FILTER_KEYS = [
  'range',
  'app',
  'release',
  'type',
  'feature',
  'userId',
  'route',
  'search',
  'onlyErrors',
] as const;

/**
 * Barra de filtros do explorador. O estado vive na URL (querystring):
 * telas são compartilháveis e o botão voltar funciona.
 */
export function FiltersBar() {
  const router = useRouter();
  const params = useSearchParams();
  const { data: options } = useQuery({ queryKey: ['filters'], queryFn: api.filterOptions });

  const [search, setSearch] = useState(params.get('search') ?? '');
  const [userId, setUserId] = useState(params.get('userId') ?? '');
  const [route, setRoute] = useState(params.get('route') ?? '');
  useEffect(() => setSearch(params.get('search') ?? ''), [params]);
  useEffect(() => setUserId(params.get('userId') ?? ''), [params]);
  useEffect(() => setRoute(params.get('route') ?? ''), [params]);

  const setParam = useCallback(
    (patch: Record<string, string>) => {
      const next = new URLSearchParams(params.toString());
      for (const [key, value] of Object.entries(patch)) {
        if (value) next.set(key, value);
        else next.delete(key);
      }
      next.delete('cursor'); // filtro novo ⇒ volta para a primeira página
      router.replace(`?${next.toString()}`);
    },
    [params, router],
  );

  return (
    <div className="card flex flex-wrap items-end gap-3 px-4 py-3">
      <Field label="Período" className="w-40">
        <Select
          value={params.get('range') ?? '24h'}
          onChange={(e) => setParam({ range: e.target.value })}
        >
          {RANGE_PRESETS.map((p) => (
            <option key={p.key} value={p.key}>
              {p.label}
            </option>
          ))}
        </Select>
      </Field>

      <Field label="App" className="w-40">
        <Select
          value={params.get('app') ?? ''}
          onChange={(e) => setParam({ app: e.target.value })}
        >
          <option value="">todos</option>
          {options?.apps.map((a) => (
            <option key={a} value={a}>
              {a}
            </option>
          ))}
        </Select>
      </Field>

      <Field label="Tipo" className="w-40">
        <Select
          value={params.get('type') ?? ''}
          onChange={(e) => setParam({ type: e.target.value })}
        >
          <option value="">todos</option>
          {options?.types.map((t) => (
            <option key={t} value={t}>
              {t}
            </option>
          ))}
        </Select>
      </Field>

      <Field label="Funcionalidade" className="w-40">
        <Select
          value={params.get('feature') ?? ''}
          onChange={(e) => setParam({ feature: e.target.value })}
        >
          <option value="">todas</option>
          {options?.features.map((f) => (
            <option key={f} value={f}>
              {f}
            </option>
          ))}
        </Select>
      </Field>

      <form
        className="w-44"
        onSubmit={(e) => {
          e.preventDefault();
          setParam({ userId });
        }}
      >
        <Field label="Usuário (userId)" htmlFor="f-user">
          <Input
            id="f-user"
            value={userId}
            placeholder="ex.: user-42"
            onChange={(e) => setUserId(e.target.value)}
          />
        </Field>
      </form>

      <form
        className="w-44"
        onSubmit={(e) => {
          e.preventDefault();
          setParam({ route });
        }}
      >
        <Field label="Rota (endpoint)" htmlFor="f-route">
          <Input
            id="f-route"
            value={route}
            placeholder="ex.: /faturas"
            onChange={(e) => setRoute(e.target.value)}
          />
        </Field>
      </form>

      <form
        className="w-44"
        onSubmit={(e) => {
          e.preventDefault();
          setParam({ search });
        }}
      >
        <Field label="Busca livre" htmlFor="f-search">
          <Input
            id="f-search"
            value={search}
            placeholder="nome, tela, erro…"
            onChange={(e) => setSearch(e.target.value)}
          />
        </Field>
      </form>

      <div className="mb-1.5 flex items-center gap-2">
        <Checkbox
          checked={params.get('onlyErrors') === 'true'}
          onChange={(e) => setParam({ onlyErrors: e.target.checked ? 'true' : '' })}
          label="Somente erros"
        />
      </div>

      <div className="mb-0.5 flex items-center gap-2">
        <Button variant="primary" onClick={() => setParam({ search, userId, route })}>
          Consultar
        </Button>
        <Button variant="ghost" size="sm" onClick={() => router.replace('?')}>
          limpar
        </Button>
        <SavedViewsMenu
          viewType="traces"
          currentFilters={collectFilters(params)}
          onApply={(filters) => {
            // Aplica todos os campos conhecidos + limpa os que a view não define.
            const patch: Record<string, string> = {};
            for (const k of VIEW_FILTER_KEYS) {
              const v = filters[k];
              patch[k] = typeof v === 'string' ? v : '';
            }
            setParam(patch);
          }}
        />
      </div>
    </div>
  );
}

// collectFilters extrai só as chaves relevantes do querystring atual — assim
// não salvamos cursor de paginação ou outras props transientes na view.
function collectFilters(params: URLSearchParams): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const k of VIEW_FILTER_KEYS) {
    const v = params.get(k);
    if (v) out[k] = v;
  }
  return out;
}
