'use client';

import { usePathname } from 'next/navigation';
import { Select } from '@/components/form';
import { useAppScope } from '@/lib/app-scope';

/**
 * Barra global de escopo de App.
 *
 * Fica visível em todas as telas de observabilidade quando o usuário tem mais
 * de um app. A escolha é persistida pelo AppScopeProvider, então navegar entre
 * Issues, Performance, Traces etc. mantém o mesmo app.
 *
 * Admin fica de fora: as telas de administração gerenciam apps/chaves de
 * forma global, não devem ser filtradas por um app específico.
 */
export function AppScopeBar() {
  const pathname = usePathname();
  const { apps, selectedApp, setSelectedApp, hasMultipleApps } = useAppScope();

  if (!hasMultipleApps || pathname.startsWith('/admin')) return null;

  return (
    <div className="sticky top-0 z-30 border-b border-hairline bg-plane/95 backdrop-blur">
      <div className="mx-auto flex max-w-7xl flex-wrap items-center gap-x-3 gap-y-1 px-6 py-2">
        <span className="text-[10px] font-semibold uppercase tracking-wider text-muted">
          App
        </span>
        <Select
          aria-label="App em análise"
          value={selectedApp}
          onChange={(e) => setSelectedApp(e.target.value)}
          className="w-52"
        >
          <option value="">Todos os apps</option>
          {apps.map((app) => (
            <option key={app} value={app}>
              {app}
            </option>
          ))}
        </Select>
        <span className="text-xs text-muted">
          {selectedApp
            ? `Toda a navegação está filtrada por ${selectedApp}`
            : 'Toda a navegação está sem filtro de app'}
        </span>
      </div>
    </div>
  );
}
