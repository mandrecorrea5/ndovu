import type { Metadata } from 'next';
import './globals.css';
import { AppShell } from '@/components/AppShell';
import { Providers } from './providers';

export const metadata: Metadata = {
  title: 'Ndovu — Frontend Tracing',
  description: 'Rastreamento de jornada de usuário em frontends',
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="pt-BR">
      <body className="min-h-screen">
        <Providers>
          <AppShell>{children}</AppShell>
        </Providers>
      </body>
    </html>
  );
}
