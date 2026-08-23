import type { Config } from 'tailwindcss';

/**
 * As cores vêm de CSS custom properties definidas em globals.css
 * (paleta validada para CVD, com variantes light/dark) — o Tailwind
 * só as referencia por papel semântico.
 */
const config: Config = {
  content: ['./src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        surface: 'var(--surface-1)',
        plane: 'var(--plane)',
        ink: 'var(--text-primary)',
        'ink-2': 'var(--text-secondary)',
        muted: 'var(--text-muted)',
        grid: 'var(--gridline)',
        hairline: 'var(--hairline)',
        accent: 'var(--series-1)',
        danger: 'var(--series-err)',
        good: 'var(--status-good)',
        warn: 'var(--status-warning)',
        critical: 'var(--status-critical)',
      },
    },
  },
  plugins: [],
};

export default config;
