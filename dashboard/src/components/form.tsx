'use client';

import type {
  ButtonHTMLAttributes,
  InputHTMLAttributes,
  ReactNode,
  SelectHTMLAttributes,
  TextareaHTMLAttributes,
} from 'react';

/**
 * Design system de formulários — todos os inputs/selects/botões e wrappers de
 * campo passam por aqui. Um só lugar para ajustar tipografia, altura, focus
 * ring, estados disabled/inválido. Antes disso cada tela tinha seu inputCls
 * hardcoded e o resultado era inconsistente.
 */

// ---------------------------------------------------------------------------
// Tokens compartilhados — mudar aqui reflete em toda a UI
// ---------------------------------------------------------------------------

const CONTROL_BASE =
  'block w-full rounded-md border border-hairline bg-surface px-3 py-2 text-sm text-ink ' +
  'placeholder:text-muted transition-colors ' +
  'hover:border-ink-2/40 ' +
  'focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/30 ' +
  'disabled:cursor-not-allowed disabled:opacity-50';

const CONTROL_INVALID =
  'border-critical/60 focus:border-critical focus:ring-critical/30';

// ---------------------------------------------------------------------------
// Field — label + control + hint/erro consistentes
// ---------------------------------------------------------------------------

interface FieldProps {
  label: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
  required?: boolean;
  htmlFor?: string;
  children: ReactNode;
  className?: string;
}

export function Field({ label, hint, error, required, htmlFor, children, className }: FieldProps) {
  return (
    <div className={`flex flex-col gap-1 ${className ?? ''}`}>
      <label
        htmlFor={htmlFor}
        className="text-xs font-medium uppercase tracking-wide text-muted"
      >
        {label}
        {required ? <span className="ml-0.5 text-critical">*</span> : null}
      </label>
      {children}
      {error ? (
        <span className="text-xs text-critical">{error}</span>
      ) : hint ? (
        <span className="text-xs text-muted">{hint}</span>
      ) : null}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Input
// ---------------------------------------------------------------------------

interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  invalid?: boolean;
}

export function Input({ invalid, className, ...rest }: InputProps) {
  return (
    <input
      className={`${CONTROL_BASE} ${invalid ? CONTROL_INVALID : ''} ${className ?? ''}`}
      {...rest}
    />
  );
}

// ---------------------------------------------------------------------------
// Select — mesma altura/estilo do Input; seta custom (evita o feio do OS)
// ---------------------------------------------------------------------------

interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  invalid?: boolean;
}

export function Select({ invalid, className, children, ...rest }: SelectProps) {
  return (
    <div className="relative">
      <select
        className={`${CONTROL_BASE} appearance-none pr-8 ${invalid ? CONTROL_INVALID : ''} ${className ?? ''}`}
        {...rest}
      >
        {children}
      </select>
      <span
        className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-muted"
        aria-hidden
      >
        ▾
      </span>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Textarea
// ---------------------------------------------------------------------------

interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  invalid?: boolean;
}

export function Textarea({ invalid, className, ...rest }: TextareaProps) {
  return (
    <textarea
      className={`${CONTROL_BASE} min-h-[80px] resize-y ${invalid ? CONTROL_INVALID : ''} ${className ?? ''}`}
      {...rest}
    />
  );
}

// ---------------------------------------------------------------------------
// Checkbox — com label ao lado, um controle atômico
// ---------------------------------------------------------------------------

interface CheckboxProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'type'> {
  label: ReactNode;
}

export function Checkbox({ label, className, ...rest }: CheckboxProps) {
  return (
    <label className={`inline-flex cursor-pointer items-center gap-2 text-sm text-ink-2 ${className ?? ''}`}>
      <input
        type="checkbox"
        className="h-4 w-4 rounded border-hairline text-accent focus:ring-2 focus:ring-accent/30"
        {...rest}
      />
      {label}
    </label>
  );
}

// ---------------------------------------------------------------------------
// Button — variants primary/secondary/danger/ghost + sizes sm/md
// ---------------------------------------------------------------------------

type ButtonVariant = 'primary' | 'secondary' | 'danger' | 'ghost';
type ButtonSize = 'sm' | 'md';

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  loading?: boolean;
  icon?: ReactNode;
}

const BUTTON_BASE =
  'inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-md font-medium transition-colors ' +
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40 ' +
  'disabled:cursor-not-allowed disabled:opacity-50';

const BUTTON_VARIANT: Record<ButtonVariant, string> = {
  primary: 'bg-accent text-white hover:opacity-90',
  secondary: 'border border-hairline bg-surface text-ink-2 hover:bg-plane hover:text-ink',
  danger: 'border border-critical/40 text-critical hover:bg-critical/10',
  ghost: 'text-ink-2 hover:bg-plane hover:text-ink',
};

const BUTTON_SIZE: Record<ButtonSize, string> = {
  sm: 'h-7 px-2.5 text-xs',
  md: 'h-9 px-3.5 text-sm',
};

export function Button({
  variant = 'secondary',
  size = 'md',
  loading,
  icon,
  disabled,
  className,
  children,
  ...rest
}: ButtonProps) {
  return (
    <button
      type="button"
      disabled={disabled || loading}
      className={`${BUTTON_BASE} ${BUTTON_VARIANT[variant]} ${BUTTON_SIZE[size]} ${className ?? ''}`}
      {...rest}
    >
      {icon && !loading ? <span aria-hidden>{icon}</span> : null}
      {loading ? (
        <span className="inline-block h-3 w-3 animate-spin rounded-full border-2 border-current border-t-transparent" />
      ) : null}
      {children}
    </button>
  );
}
