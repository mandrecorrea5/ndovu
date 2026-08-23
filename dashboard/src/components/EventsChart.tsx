'use client';

import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import type { TimeBucket } from '@/lib/types';
import { fmtDateTime } from '@/lib/time';

/**
 * Série temporal de eventos vs erros.
 * Cores por papel (CSS vars da paleta validada): total = série 1 (azul),
 * erros = série de erro (vermelho). Legenda sempre presente (2 séries).
 */
export function EventsChart({ series }: { series: TimeBucket[] }) {
  const data = series.map((b) => ({
    ...b,
    label: fmtDateTime(b.bucket),
  }));

  return (
    <div className="h-64 w-full" role="img" aria-label="Eventos e erros ao longo do tempo">
      <ResponsiveContainer>
        <LineChart data={data} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
          <CartesianGrid stroke="var(--gridline)" strokeWidth={1} vertical={false} />
          <XAxis
            dataKey="label"
            tick={{ fill: 'var(--text-muted)', fontSize: 11 }}
            tickLine={false}
            axisLine={{ stroke: 'var(--baseline)' }}
            minTickGap={48}
          />
          <YAxis
            tick={{ fill: 'var(--text-muted)', fontSize: 11 }}
            tickLine={false}
            axisLine={false}
            width={40}
            allowDecimals={false}
          />
          <Tooltip
            contentStyle={{
              background: 'var(--surface-1)',
              border: '1px solid var(--hairline)',
              borderRadius: 8,
              color: 'var(--text-primary)',
              fontSize: 12,
            }}
            labelStyle={{ color: 'var(--text-secondary)' }}
          />
          <Legend
            formatter={(value: string) => (
              <span style={{ color: 'var(--text-secondary)', fontSize: 12 }}>{value}</span>
            )}
          />
          <Line
            type="monotone"
            dataKey="total"
            name="Eventos"
            stroke="var(--series-1)"
            strokeWidth={2}
            dot={false}
            activeDot={{ r: 4 }}
          />
          <Line
            type="monotone"
            dataKey="errors"
            name="Erros"
            stroke="var(--series-err)"
            strokeWidth={2}
            dot={false}
            activeDot={{ r: 4 }}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
