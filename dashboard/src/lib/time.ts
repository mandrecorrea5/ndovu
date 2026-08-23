/** Presets de janela de tempo e formatação local. */

export interface RangePreset {
  key: string;
  label: string;
  hours: number;
}

export const RANGE_PRESETS: RangePreset[] = [
  { key: '1h', label: 'Última hora', hours: 1 },
  { key: '6h', label: 'Últimas 6h', hours: 6 },
  { key: '24h', label: 'Últimas 24h', hours: 24 },
  { key: '7d', label: 'Últimos 7 dias', hours: 24 * 7 },
  { key: '30d', label: 'Últimos 30 dias', hours: 24 * 30 },
];

export function rangeToInterval(key: string): { from: string; to: string } {
  const preset = RANGE_PRESETS.find((p) => p.key === key) ?? RANGE_PRESETS[2];
  const to = new Date();
  const from = new Date(to.getTime() - preset.hours * 3600_000);
  return { from: from.toISOString(), to: to.toISOString() };
}

export function fmtDateTime(iso: string): string {
  return new Date(iso).toLocaleString('pt-BR', {
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

export function fmtTime(iso: string): string {
  return new Date(iso).toLocaleTimeString('pt-BR', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

export function fmtDuration(ms?: number): string {
  if (ms == null) return '—';
  if (ms < 1000) return `${ms} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}

export function fmtNumber(n: number): string {
  return n.toLocaleString('pt-BR');
}
