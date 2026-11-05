'use client';

import { useQuery } from '@tanstack/react-query';
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import { api } from './api';

/**
 * Escopo global de App do dashboard.
 *
 * O usuário escolhe o app UMA vez (no seletor do AppShell) e todas as telas
 * passam a consultar aquele app. A escolha fica em localStorage por usuário.
 *
 * Regras:
 *   - mais de um app acessível → seletor visível, com opção "todos os apps";
 *   - exatamente um app → seleção automática e seletor escondido (a navegação
 *     "fica normal", sem escolha);
 *   - nenhum app → comportamento anterior (todos, que o backend limita ao
 *     escopo do usuário).
 *
 * A lista vem de /v1/meta/apps, que já aplica o tenantScope do backend
 * (company + grants explícitos por usuário).
 */

const STORAGE_PREFIX = 'ndovu.selected-app.';

interface AppScopeValue {
  /** Apps que o usuário autenticado pode consultar. */
  apps: string[];
  /** App selecionado; string vazia = todos os apps. */
  selectedApp: string;
  /** true quando há mais de um app acessível (seletor deve aparecer). */
  hasMultipleApps: boolean;
  /** true enquanto a lista de apps ainda está carregando. */
  loading: boolean;
  setSelectedApp: (app: string) => void;
}

const AppScopeContext = createContext<AppScopeValue | null>(null);

function readStored(key: string): string {
  if (typeof window === 'undefined') return '';
  try {
    return window.localStorage.getItem(key) ?? '';
  } catch {
    return '';
  }
}

export function AppScopeProvider({ userId, children }: { userId: string; children: ReactNode }) {
  const storageKey = `${STORAGE_PREFIX}${userId}`;
  // Inicializa de forma síncrona: evita a primeira query "sem app" quando o
  // usuário já tinha escolhido um app numa visita anterior.
  const [preference, setPreference] = useState(() => readStored(storageKey));

  const { data, isLoading } = useQuery({
    queryKey: ['my-apps', userId],
    queryFn: api.myApps,
    staleTime: 5 * 60_000,
  });

  // Mantém abas abertas em sincronia: trocar o app numa aba reflete nas outras.
  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === storageKey) setPreference(e.newValue ?? '');
    };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, [storageKey]);

  const apps = useMemo(() => data?.apps ?? [], [data]);

  const selectedApp = useMemo(() => {
    // Enquanto a lista não chegou, mantém a preferência salva para a primeira
    // query já sair filtrada. O backend aplica o tenantScope de qualquer forma.
    if (apps.length === 0) return preference;
    // Preferência do usuário, desde que ele ainda tenha acesso ao app.
    if (preference && apps.includes(preference)) return preference;
    // Sem escolha explícita: se só existe um app, não faz sentido perguntar.
    if (apps.length === 1) return apps[0];
    return '';
  }, [apps, preference]);

  const setSelectedApp = useCallback(
    (app: string) => {
      setPreference(app);
      if (typeof window === 'undefined') return;
      try {
        if (app) window.localStorage.setItem(storageKey, app);
        else window.localStorage.removeItem(storageKey);
      } catch {
        /* modo privado / storage cheio — a seleção vale só nesta aba */
      }
    },
    [storageKey],
  );

  const value = useMemo<AppScopeValue>(
    () => ({
      apps,
      selectedApp,
      hasMultipleApps: apps.length > 1,
      loading: isLoading,
      setSelectedApp,
    }),
    [apps, selectedApp, isLoading, setSelectedApp],
  );

  return <AppScopeContext.Provider value={value}>{children}</AppScopeContext.Provider>;
}

export function useAppScope(): AppScopeValue {
  const ctx = useContext(AppScopeContext);
  if (!ctx) {
    throw new Error('useAppScope deve ser usado dentro de AppScopeProvider');
  }
  return ctx;
}
