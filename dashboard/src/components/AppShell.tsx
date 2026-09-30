'use client';

import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import {
  clearSession,
  fetchSession,
  getSessionUser,
  logout as endSession,
  type SessionUser,
} from '@/lib/auth';

// Ícones SVG inline — evita adicionar lucide-react/heroicons como dependência.
const Icon = {
  overview: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="3" width="7" height="9" rx="1"/><rect x="14" y="3" width="7" height="5" rx="1"/><rect x="14" y="12" width="7" height="9" rx="1"/><rect x="3" y="16" width="7" height="5" rx="1"/></svg>
  ),
  issues: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M12 9v4"/><path d="M12 17h.01"/><path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0Z"/></svg>
  ),
  performance: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M12 2v4"/><path d="m16.24 7.76 2.83-2.83"/><path d="M18 12h4"/><circle cx="12" cy="14" r="8"/><path d="M12 14v-4"/></svg>
  ),
  traces: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M4 6h16"/><path d="M4 12h10"/><path d="M4 18h16"/><circle cx="18" cy="12" r="2"/></svg>
  ),
  sessions: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>
  ),
  funnels: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M22 3H2l8 9.46V19l4 2v-8.54L22 3z"/></svg>
  ),
  retention: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/><polyline points="21 4 21 10 15 10"/></svg>
  ),
  releases: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M20.59 13.41 13.42 20.58a2 2 0 0 1-2.83 0L2 12V2h10l8.59 8.59a2 2 0 0 1 0 2.82Z"/><line x1="7" x2="7.01" y1="7" y2="7"/></svg>
  ),
  sourceMaps: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5L14.5 2z"/><polyline points="14 2 14 8 20 8"/><path d="m10 13-2 2 2 2"/><path d="m14 17 2-2-2-2"/></svg>
  ),
  companies: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><rect x="4" y="2" width="16" height="20" rx="2"/><path d="M9 22v-4h6v4"/><path d="M8 6h.01"/><path d="M16 6h.01"/><path d="M12 6h.01"/><path d="M8 10h.01"/><path d="M12 10h.01"/><path d="M16 10h.01"/><path d="M8 14h.01"/><path d="M12 14h.01"/><path d="M16 14h.01"/></svg>
  ),
  apps: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>
  ),
  users: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>
  ),
  keys: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><circle cx="7.5" cy="15.5" r="5.5"/><path d="m21 2-9.6 9.6"/><path d="m15.5 7.5 3 3L22 7l-3-3"/></svg>
  ),
  alerts: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9"/><path d="M10.3 21a1.94 1.94 0 0 0 3.4 0"/></svg>
  ),
  audit: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" x2="8" y1="13" y2="13"/><line x1="16" x2="8" y1="17" y2="17"/><line x1="10" x2="8" y1="9" y2="9"/></svg>
  ),
  gdpr: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>
  ),
  sampling: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M3 3v18h18"/><path d="M7 14l4-4 4 3 5-6"/></svg>
  ),
  anomaly: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M3 12h4l3-9 4 18 3-9h4"/></svg>
  ),
  feedback: (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>
  ),
  collapseLeft: (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="m15 18-6-6 6-6"/></svg>
  ),
  collapseRight: (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="m9 18 6-6-6-6"/></svg>
  ),
  logout: (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" x2="9" y1="12" y2="12"/></svg>
  ),
};

interface NavItem {
  href: string;
  label: string;
  icon: ReactNode;
  section?: 'main' | 'admin';
}

const NAV: NavItem[] = [
  { href: '/', label: 'Visão geral', icon: Icon.overview },
  { href: '/issues', label: 'Issues', icon: Icon.issues },
  { href: '/releases', label: 'Releases', icon: Icon.releases },
  { href: '/performance', label: 'Performance', icon: Icon.performance },
  { href: '/funnels', label: 'Funis', icon: Icon.funnels },
  { href: '/retention', label: 'Retenção', icon: Icon.retention },
  { href: '/traces', label: 'Explorador', icon: Icon.traces },
  { href: '/sessions', label: 'Sessões', icon: Icon.sessions },
];

const ADMIN_NAV: NavItem[] = [
  { href: '/admin/companies', label: 'Empresas', icon: Icon.companies },
  { href: '/admin/apps', label: 'Apps', icon: Icon.apps },
  { href: '/admin/users', label: 'Usuários', icon: Icon.users },
  { href: '/admin/keys', label: 'Chaves de API', icon: Icon.keys },
  { href: '/admin/alerts', label: 'Alertas', icon: Icon.alerts },
  { href: '/admin/source-maps', label: 'Source maps', icon: Icon.sourceMaps },
  { href: '/admin/audit-log', label: 'Audit log', icon: Icon.audit },
  { href: '/admin/gdpr', label: 'LGPD', icon: Icon.gdpr },
  { href: '/admin/sampling', label: 'Sampling', icon: Icon.sampling },
  { href: '/admin/anomalies', label: 'Anomalias', icon: Icon.anomaly },
  { href: '/admin/feedbacks', label: 'Feedback', icon: Icon.feedback },
];

const COLLAPSED_KEY = 'ndovu.sidebar.collapsed';

/**
 * Casca da aplicação: sidebar lateral colapsível + área principal. Protege as
 * rotas no cliente (sem token → /login); o servidor protege de verdade (401).
 */
export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const [user, setUser] = useState<SessionUser | null>(null);
  const [ready, setReady] = useState(false);
  const [collapsed, setCollapsed] = useState(false);
  const isLogin = pathname.startsWith('/login');

  useEffect(() => {
    // Gate client-side é cosmético: a autoridade é o BFF (401 → redirect no
    // api client). Confirmamos a sessão no /api/auth/me para não mostrar
    // shell com cache de outro usuário ou sessão já revogada.
    if (isLogin) {
      setReady(true);
      return;
    }
    let cancelled = false;
    (async () => {
      const cached = getSessionUser();
      if (cached && !cancelled) setUser(cached);
      const fresh = await fetchSession();
      if (cancelled) return;
      if (!fresh) {
        const next = encodeURIComponent(pathname);
        router.replace(`/login?next=${next}`);
        return;
      }
      setUser(fresh);
      setReady(true);
    })();
    return () => {
      cancelled = true;
    };
  }, [isLogin, pathname, router]);

  const toggleCollapsed = () => {
    setCollapsed((prev) => {
      const next = !prev;
      try {
        localStorage.setItem(COLLAPSED_KEY, next ? '1' : '0');
      } catch {
        /* ignore */
      }
      return next;
    });
  };

  const logout = async () => {
    await endSession();
    clearSession();
    router.replace('/login');
  };

  if (isLogin) return <>{children}</>;
  if (!ready) return null;

  const width = collapsed ? 'w-16' : 'w-60';

  return (
    <div className="flex min-h-screen">
      <aside
        className={`${width} sticky top-0 flex h-screen flex-col border-r border-hairline bg-surface transition-[width] duration-150`}
      >
        <div className="flex h-14 items-center gap-2 border-b border-hairline px-3">
          <Link href="/" className="flex items-center gap-2 truncate text-base font-semibold">
            <span aria-hidden className="text-lg">🐘</span>
            {!collapsed ? <span>Ndovu</span> : null}
          </Link>
          <button
            type="button"
            onClick={toggleCollapsed}
            aria-label={collapsed ? 'Expandir menu' : 'Recolher menu'}
            className="ml-auto rounded-md p-1 text-ink-2 hover:bg-plane hover:text-ink"
          >
            {collapsed ? Icon.collapseRight : Icon.collapseLeft}
          </button>
        </div>

        <nav className="flex-1 overflow-y-auto py-3">
          <SidebarSection items={NAV} collapsed={collapsed} pathname={pathname} />
          {user?.role === 'admin' ? (
            <SidebarSection
              title="Administração"
              items={ADMIN_NAV}
              collapsed={collapsed}
              pathname={pathname}
            />
          ) : null}
        </nav>

        <UserMenu user={user} collapsed={collapsed} onLogout={logout} />
      </aside>

      <main className="flex-1 overflow-x-hidden bg-plane">
        <div className="mx-auto max-w-7xl px-6 py-6">{children}</div>
      </main>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Seção de nav (agrupa itens + título opcional)
// ---------------------------------------------------------------------------

function SidebarSection({
  title,
  items,
  collapsed,
  pathname,
}: {
  title?: string;
  items: NavItem[];
  collapsed: boolean;
  pathname: string;
}) {
  return (
    <div className="mb-3">
      {title && !collapsed ? (
        <p className="mb-1 px-3 text-[10px] font-semibold uppercase tracking-wider text-muted">
          {title}
        </p>
      ) : null}
      <ul className="flex flex-col gap-0.5 px-2">
        {items.map((item) => {
          const active = item.href === '/'
            ? pathname === '/'
            : pathname === item.href || pathname.startsWith(item.href + '/');
          return (
            <li key={item.href}>
              <Link
                href={item.href}
                title={collapsed ? item.label : undefined}
                className={`flex items-center gap-2.5 rounded-md px-2.5 py-2 text-sm transition-colors ${
                  active
                    ? 'bg-accent/10 text-accent'
                    : 'text-ink-2 hover:bg-plane hover:text-ink'
                } ${collapsed ? 'justify-center' : ''}`}
              >
                <span className="flex-shrink-0" aria-hidden>
                  {item.icon}
                </span>
                {!collapsed ? <span className="truncate">{item.label}</span> : null}
              </Link>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

// ---------------------------------------------------------------------------
// UserMenu — botão do usuário com dropdown (sair, etc.)
// ---------------------------------------------------------------------------

function UserMenu({
  user,
  collapsed,
  onLogout,
}: {
  user: SessionUser | null;
  collapsed: boolean;
  onLogout: () => void | Promise<void>;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onClick = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    document.addEventListener('mousedown', onClick);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onClick);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  const initials = (user?.name ?? user?.email ?? '?')
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((s) => s[0]?.toUpperCase())
    .join('');

  return (
    <div ref={ref} className="relative border-t border-hairline p-2">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="menu"
        aria-expanded={open}
        className={`flex w-full items-center gap-2.5 rounded-md p-2 text-left text-sm text-ink hover:bg-plane ${
          collapsed ? 'justify-center' : ''
        }`}
      >
        <span className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-accent/20 text-xs font-semibold text-accent">
          {initials || '·'}
        </span>
        {!collapsed ? (
          <span className="min-w-0 flex-1">
            <span className="block truncate font-medium">{user?.name}</span>
            <span className="block truncate text-xs text-muted">{user?.email}</span>
          </span>
        ) : null}
        {!collapsed ? (
          <span className="text-muted" aria-hidden>
            ⋯
          </span>
        ) : null}
      </button>

      {open ? (
        <div
          role="menu"
          className={`absolute z-40 min-w-[200px] rounded-md border border-hairline bg-surface p-1 shadow-xl ${
            collapsed
              ? 'bottom-2 left-full ml-2'
              : 'bottom-full left-2 right-2 mb-2'
          }`}
        >
          <div className="border-b border-hairline px-3 py-2">
            <p className="truncate text-sm font-medium text-ink">{user?.name}</p>
            <p className="truncate text-xs text-muted">{user?.email}</p>
            <p className="mt-1 text-[10px] uppercase tracking-wider text-muted">
              {user?.role}
            </p>
          </div>
          <button
            type="button"
            onClick={onLogout}
            role="menuitem"
            className="flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm text-ink-2 hover:bg-plane hover:text-ink"
          >
            <span aria-hidden>{Icon.logout}</span>
            Sair
          </button>
        </div>
      ) : null}
    </div>
  );
}
