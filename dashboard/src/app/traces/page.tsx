'use client';

import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { useRouter, useSearchParams } from 'next/navigation';
import { Suspense, useEffect, useRef, useState } from 'react';
import { EventRow, EventsTableHead } from '@/components/EventRow';
import { FiltersBar } from '@/components/FiltersBar';
import { EmptyState, ErrorState, LoadingState } from '@/components/ui';
import { api } from '@/lib/api';
import { useAppScope } from '@/lib/app-scope';
import { rangeToInterval } from '@/lib/time';
import type { EventQuery, TraceEvent } from '@/lib/types';

function TracesExplorer() {
  const params = useSearchParams();
  const router = useRouter();
  const { selectedApp, setSelectedApp } = useAppScope();
  const [pages, setPages] = useState<TraceEvent[][]>([]);
  const [cursor, setCursor] = useState<string | undefined>(undefined);

  // Deep link com ?app= (ex.: "ver eventos" de uma issue) tem prioridade no
  // primeiro render; em seguida ele é promovido a escopo global e sai da URL.
  const urlApp = params.get('app') ?? '';
  const effectiveApp = urlApp || selectedApp;
  const syncedApp = useRef('');

  useEffect(() => {
    if (!urlApp) {
      syncedApp.current = '';
      return;
    }
    if (syncedApp.current === urlApp) return;
    syncedApp.current = urlApp;
    if (urlApp !== selectedApp) setSelectedApp(urlApp);
    const next = new URLSearchParams(params.toString());
    next.delete('app');
    router.replace(`?${next.toString()}`);
  }, [urlApp, selectedApp, setSelectedApp, params, router]);

  const query: EventQuery = (() => {
    const { from, to } = rangeToInterval(params.get('range') ?? '24h');
    return {
      from,
      to,
      app: effectiveApp || undefined,
      userId: params.get('userId') ?? undefined,
      sessionId: params.get('sessionId') ?? undefined,
      type: params.get('type') ?? undefined,
      feature: params.get('feature') ?? undefined,
      route: params.get('route') ?? undefined,
      screen: params.get('screen') ?? undefined,
      onlyErrors: params.get('onlyErrors') ?? undefined,
      search: params.get('search') ?? undefined,
      limit: '50',
    };
  })();

  const queryKey = ['events', params.toString(), effectiveApp, cursor];
  const { data, isLoading, error, isFetching } = useQuery({
    queryKey,
    queryFn: async () => {
      const page = await api.events({ ...query, cursor });
      return page;
    },
    placeholderData: keepPreviousData,
  });

  // Acumula páginas carregadas via "carregar mais"
  const events = [...pages.flat(), ...(data?.events ?? [])];

  const loadMore = () => {
    if (data?.nextCursor) {
      setPages((prev) => [...prev, data.events]);
      setCursor(data.nextCursor);
    }
  };

  // Filtro mudou (querystring ou app global) ⇒ zera o acumulado
  const paramsKey = `${params.toString()}|${effectiveApp}`;
  const [lastParams, setLastParams] = useState(paramsKey);
  if (paramsKey !== lastParams) {
    setLastParams(paramsKey);
    setPages([]);
    setCursor(undefined);
  }

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">Explorador de traces</h1>
      <FiltersBar />

      {error ? <ErrorState message={(error as Error).message} /> : null}
      {isLoading ? <LoadingState /> : null}

      {!isLoading && events.length === 0 && !error ? (
        <EmptyState message="Nenhum evento com esses filtros. Ajuste o período ou limpe os filtros." />
      ) : null}

      {events.length > 0 ? (
        <div className="card overflow-x-auto">
          <table className="w-full min-w-[900px] text-sm">
            <EventsTableHead />
            <tbody>
              {events.map((e) => (
                <EventRow key={e.eventId} event={e} />
              ))}
            </tbody>
          </table>
          <div className="flex items-center justify-between border-t border-hairline px-4 py-2 text-xs text-muted">
            <span className="tabular">{events.length} eventos carregados</span>
            {data?.nextCursor ? (
              <button
                type="button"
                onClick={loadMore}
                disabled={isFetching}
                className="rounded-md border border-hairline px-3 py-1.5 text-sm text-ink-2 hover:bg-plane disabled:opacity-50"
              >
                {isFetching ? 'carregando…' : 'Carregar mais'}
              </button>
            ) : (
              <span>fim dos resultados</span>
            )}
          </div>
        </div>
      ) : null}
    </div>
  );
}

export default function TracesPage() {
  return (
    <Suspense fallback={<LoadingState />}>
      <TracesExplorer />
    </Suspense>
  );
}
