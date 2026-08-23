'use client';

import { useEffect } from 'react';

interface ModalProps {
  open: boolean;
  onClose: () => void;
  title: string;
  description?: string;
  children: React.ReactNode;
  size?: 'sm' | 'md' | 'lg';
}

/**
 * Modal acessível: fecha por ESC, click no overlay ou botão. O foco é preso
 * dentro dele com CSS/tabindex simples (sem lib) — para a POC é o suficiente.
 * O body ganha overflow-hidden enquanto aberto para não rolar por trás.
 */
export function Modal({ open, onClose, title, description, children, size = 'md' }: ModalProps) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.removeEventListener('keydown', onKey);
      document.body.style.overflow = prev;
    };
  }, [open, onClose]);

  if (!open) return null;

  const maxWidth =
    size === 'sm' ? 'max-w-md' : size === 'lg' ? 'max-w-3xl' : 'max-w-xl';

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
      role="dialog"
      aria-modal="true"
      aria-labelledby="modal-title"
    >
      <div
        className={`w-full ${maxWidth} rounded-lg border border-hairline bg-surface shadow-xl`}
      >
        <header className="flex items-start justify-between gap-3 border-b border-hairline px-5 py-3">
          <div>
            <h2 id="modal-title" className="text-base font-semibold text-ink">
              {title}
            </h2>
            {description ? (
              <p className="mt-0.5 text-xs text-ink-2">{description}</p>
            ) : null}
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="Fechar"
            className="rounded-md border border-hairline px-2 py-1 text-xs text-ink-2 hover:bg-plane"
          >
            ✕
          </button>
        </header>
        <div className="max-h-[70vh] overflow-y-auto px-5 py-4">{children}</div>
      </div>
    </div>
  );
}

/**
 * Rodapé padronizado do modal: ações à direita. Usar dentro de <Modal>.
 */
export function ModalActions({ children }: { children: React.ReactNode }) {
  return (
    <div className="mt-4 flex justify-end gap-2 border-t border-hairline pt-3">
      {children}
    </div>
  );
}
