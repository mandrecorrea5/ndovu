/**
 * Ndovu — auto-instrumentação Next.js (App Router).
 *
 * Componente client-side que observa mudanças de rota via usePathname()/
 * useSearchParams() e dispara `ndovu.pageView()` automaticamente — sem
 * tocar código nas telas.
 *
 * Uso (em app/layout.tsx):
 *
 *   'use client';
 *   import { NdovuAutoRouter, NdovuProvider } from '@your-org/ndovu-next';
 *   import { createNdovu } from '@your-org/ndovu-browser';
 *
 *   const ndovu = createNdovu({ ... });
 *
 *   export default function RootLayout({ children }) {
 *     return (
 *       <html>
 *         <body>
 *           <NdovuProvider ndovu={ndovu}>
 *             <NdovuAutoRouter />
 *             {children}
 *           </NdovuProvider>
 *         </body>
 *       </html>
 *     );
 *   }
 *
 * Sem JSX de propósito: assim o arquivo passa `node --check` do CI e pode
 * ser copiado como fonte única em qualquer projeto Next sem etapa de build.
 */

'use client';

import { usePathname, useSearchParams } from 'next/navigation';
import {
  createContext,
  createElement,
  Suspense,
  useContext,
  useEffect,
  useRef,
} from 'react';

const NdovuContext = createContext(null);

/**
 * Provider opcional: expõe a instância do SDK via useNdovu(). Se você já
 * criou o SDK em outro lugar e quer só o autotracking, pode passar o
 * `ndovu` direto em NdovuAutoRouter e pular o provider.
 */
export function NdovuProvider({ ndovu, children }) {
  return createElement(NdovuContext.Provider, { value: ndovu }, children);
}

/** Hook para consumir a instância dentro de componentes client. */
export function useNdovu() {
  return useContext(NdovuContext);
}

/**
 * Observa mudanças de rota e dispara pageView automaticamente. Recebe o
 * SDK por prop OU consome do NdovuProvider. useSearchParams exige Suspense.
 *
 * Props:
 *   - ndovu?: instância do SDK (fallback: NdovuProvider)
 *   - includeSearch?: bool — se true, considera a querystring como parte da
 *     rota (dispara pageView de novo quando um filtro muda). Default: false
 *   - screenName?: (pathname) => string — customiza o nome da tela enviado
 */
export function NdovuAutoRouter(props) {
  return createElement(Suspense, { fallback: null }, createElement(RouterInner, props));
}

function RouterInner({ ndovu: ndovuProp, includeSearch = false, screenName }) {
  const ctx = useContext(NdovuContext);
  const ndovu = ndovuProp ?? ctx;
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const lastRef = useRef(null);

  useEffect(() => {
    if (!ndovu || !pathname) return;
    const search = searchParams.toString();
    const key = includeSearch && search ? `${pathname}?${search}` : pathname;
    if (lastRef.current === key) return;
    lastRef.current = key;
    const screen = typeof screenName === 'function' ? screenName(pathname) : pathname;
    const extra = includeSearch ? { metadata: { query: Object.fromEntries(searchParams) } } : {};
    ndovu.pageView(screen, extra);
  }, [ndovu, pathname, searchParams, includeSearch, screenName]);

  return null;
}
