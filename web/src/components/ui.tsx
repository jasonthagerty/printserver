import type { ButtonHTMLAttributes, ReactNode } from "react";
import { AlertIcon, CheckIcon } from "./icons";

export function cx(...c: (string | false | null | undefined)[]) {
  return c.filter(Boolean).join(" ");
}

export function Card({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <section
      className={cx(
        "rounded-3xl bg-white p-5 shadow-sm ring-1 ring-slate-900/5 dark:bg-slate-900 dark:ring-white/10",
        className,
      )}
    >
      {children}
    </section>
  );
}

export function SectionTitle({ children }: { children: ReactNode }) {
  return (
    <h2 className="mb-3 text-sm font-semibold tracking-wide text-slate-500 uppercase dark:text-slate-400">
      {children}
    </h2>
  );
}

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "secondary" | "ghost" | "danger";
  busy?: boolean;
  icon?: ReactNode;
};

export function Button({
  variant = "secondary",
  busy,
  icon,
  className,
  children,
  disabled,
  ...rest
}: ButtonProps) {
  return (
    <button
      type="button"
      disabled={disabled || busy}
      className={cx(
        "inline-flex min-h-12 items-center justify-center gap-2 rounded-2xl px-5 text-base font-semibold transition select-none active:scale-[0.98] disabled:opacity-50 disabled:active:scale-100",
        variant === "primary" &&
          "bg-teal-600 text-white shadow-sm hover:bg-teal-700 dark:bg-teal-500 dark:text-slate-950 dark:hover:bg-teal-400",
        variant === "secondary" &&
          "bg-slate-100 text-slate-900 hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-100 dark:hover:bg-slate-700",
        variant === "ghost" &&
          "text-teal-700 hover:bg-teal-50 dark:text-teal-300 dark:hover:bg-slate-800",
        variant === "danger" &&
          "bg-rose-50 text-rose-700 hover:bg-rose-100 dark:bg-rose-950/50 dark:text-rose-300 dark:hover:bg-rose-950",
        className,
      )}
      {...rest}
    >
      {busy ? <Spinner /> : icon}
      {children}
    </button>
  );
}

export function Spinner({ className }: { className?: string }) {
  return (
    <span
      role="status"
      aria-label="Working"
      className={cx(
        "inline-block size-5 animate-spin rounded-full border-2 border-current border-r-transparent",
        className,
      )}
    />
  );
}

export function Segmented<T extends string | number>({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: T;
  options: { value: T; label: string; hint?: string }[];
  onChange: (v: T) => void;
}) {
  return (
    <fieldset>
      <legend className="mb-2 text-sm font-medium text-slate-600 dark:text-slate-300">
        {label}
      </legend>
      <div className="grid auto-cols-fr grid-flow-col gap-1 rounded-2xl bg-slate-100 p-1 dark:bg-slate-800">
        {options.map((o) => {
          const on = o.value === value;
          return (
            <button
              key={String(o.value)}
              type="button"
              aria-pressed={on}
              onClick={() => onChange(o.value)}
              className={cx(
                "min-h-11 rounded-xl px-2 text-sm font-semibold transition",
                on
                  ? "bg-white text-slate-900 shadow-sm dark:bg-slate-950 dark:text-white"
                  : "text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200",
              )}
            >
              <span className="block">{o.label}</span>
              {o.hint && (
                <span className="block text-xs font-normal opacity-70">
                  {o.hint}
                </span>
              )}
            </button>
          );
        })}
      </div>
    </fieldset>
  );
}

export function Select({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: { value: string; label: string }[];
  onChange: (v: string) => void;
}) {
  return (
    <label className="block">
      <span className="mb-2 block text-sm font-medium text-slate-600 dark:text-slate-300">
        {label}
      </span>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="min-h-12 w-full appearance-none rounded-2xl bg-slate-100 px-4 text-base font-medium text-slate-900 ring-teal-500 outline-none focus:ring-2 dark:bg-slate-800 dark:text-slate-100"
      >
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </label>
  );
}

export function Stepper({
  label,
  value,
  min,
  max,
  onChange,
}: {
  label: string;
  value: number;
  min: number;
  max: number;
  onChange: (v: number) => void;
}) {
  const btn =
    "grid size-12 place-items-center rounded-2xl bg-slate-100 text-2xl font-semibold text-slate-800 disabled:opacity-40 dark:bg-slate-800 dark:text-slate-100";
  return (
    <div>
      <span className="mb-2 block text-sm font-medium text-slate-600 dark:text-slate-300">
        {label}
      </span>
      <div className="flex items-center gap-3">
        <button
          type="button"
          className={btn}
          aria-label={`Fewer ${label.toLowerCase()}`}
          disabled={value <= min}
          onClick={() => onChange(Math.max(min, value - 1))}
        >
          −
        </button>
        <span
          className="min-w-10 text-center text-2xl font-semibold tabular-nums"
          aria-live="polite"
        >
          {value}
        </span>
        <button
          type="button"
          className={btn}
          aria-label={`More ${label.toLowerCase()}`}
          disabled={value >= max}
          onClick={() => onChange(Math.min(max, value + 1))}
        >
          +
        </button>
      </div>
    </div>
  );
}

export function TextField({
  label,
  hint,
  ...rest
}: {
  label: string;
  hint?: string;
} & React.InputHTMLAttributes<HTMLInputElement>) {
  return (
    <label className="block">
      <span className="mb-2 block text-sm font-medium text-slate-600 dark:text-slate-300">
        {label}
      </span>
      <input
        className="min-h-12 w-full rounded-2xl bg-slate-100 px-4 text-base text-slate-900 ring-teal-500 outline-none placeholder:text-slate-400 focus:ring-2 dark:bg-slate-800 dark:text-slate-100"
        {...rest}
      />
      {hint && (
        <span className="mt-1.5 block text-sm text-slate-500 dark:text-slate-400">
          {hint}
        </span>
      )}
    </label>
  );
}

export function Banner({
  tone,
  children,
}: {
  tone: "error" | "warning" | "success" | "info";
  children: ReactNode;
}) {
  return (
    <div
      role={tone === "error" ? "alert" : "status"}
      className={cx(
        "flex items-start gap-3 rounded-2xl px-4 py-3 text-sm font-medium",
        tone === "error" &&
          "bg-rose-50 text-rose-800 dark:bg-rose-950/60 dark:text-rose-200",
        tone === "warning" &&
          "bg-amber-50 text-amber-900 dark:bg-amber-950/60 dark:text-amber-200",
        tone === "success" &&
          "bg-emerald-50 text-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-200",
        tone === "info" &&
          "bg-sky-50 text-sky-900 dark:bg-sky-950/60 dark:text-sky-200",
      )}
    >
      {tone === "success" ? (
        <CheckIcon className="mt-0.5 size-5 shrink-0" />
      ) : tone === "info" ? null : (
        <AlertIcon className="mt-0.5 size-5 shrink-0" />
      )}
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}

export function PageHeader({
  title,
  subtitle,
  action,
}: {
  title: string;
  subtitle?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <header className="mb-5 flex items-end justify-between gap-3 px-1">
      <div className="min-w-0">
        <h1 className="text-3xl font-bold tracking-tight text-slate-900 dark:text-white">
          {title}
        </h1>
        {subtitle && (
          <p className="mt-1 text-slate-500 dark:text-slate-400">{subtitle}</p>
        )}
      </div>
      {action}
    </header>
  );
}

export function formatBytes(n: number) {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

export function timeAgo(iso: string | null | undefined, now = Date.now()) {
  if (!iso) return "";
  const s = Math.round((new Date(iso).getTime() - now) / 1000);
  const a = Math.abs(s);
  if (a < 45) return "just now";
  if (a < 3600) return rtf.format(Math.round(s / 60), "minute");
  if (a < 86400) return rtf.format(Math.round(s / 3600), "hour");
  if (a < 7 * 86400) return rtf.format(Math.round(s / 86400), "day");
  return new Date(iso).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
  });
}
