'use client';

/** Visualizador de payload JSON com copy — usado nos detalhes de evento. */
export function JsonView({ title, data }: { title: string; data: unknown }) {
  if (data == null) return null;
  const text = JSON.stringify(data, null, 2);
  return (
    <div className="mt-2">
      <div className="flex items-center justify-between">
        <p className="text-xs font-medium uppercase tracking-wide text-muted">{title}</p>
        <button
          type="button"
          onClick={() => navigator.clipboard?.writeText(text)}
          className="rounded px-1.5 py-0.5 text-xs text-ink-2 hover:bg-plane"
        >
          copiar
        </button>
      </div>
      <pre className="mono mt-1 max-h-64 overflow-auto rounded-md border border-hairline bg-plane p-3 text-xs leading-relaxed">
        {text}
      </pre>
    </div>
  );
}
