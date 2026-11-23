import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, type Supplies } from "../api/client";
import { useSupplies } from "../queries";
import { Banner, Button, Card, SectionTitle, cx, timeAgo } from "./ui";

// Cartridge ages and how long the printer has sat unused: on an inkjet
// that prints rarely, both predict dried-out ink better than ink levels.
export function Cartridges() {
  const supplies = useSupplies();
  const qc = useQueryClient();
  const testPage = useMutation({
    mutationFn: api.printTestPage,
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["jobs"] }),
  });
  const s = supplies.data;
  if (!s) return null;

  return (
    <Card>
      <SectionTitle>Ink cartridges</SectionTitle>
      <ul className="space-y-4">
        {s.cartridges.map((c) => (
          <CartridgeRow key={c.id} cartridge={c} warnMonths={s.warn_months} />
        ))}
      </ul>
      {s.ink_checked && (
        <p className="mt-3 text-xs text-slate-500 dark:text-slate-400">
          Ink levels from the printer, checked {timeAgo(s.ink_checked)}.
        </p>
      )}
      <div className="mt-5 border-t border-slate-100 pt-4 dark:border-slate-800">
        <p className="text-slate-600 dark:text-slate-300">{lastPrinted(s)}</p>
        {s.nudge && (
          <div className="mt-3">
            <Banner tone="warning">
              Printing a page now and then keeps the ink from drying in the
              nozzles.
            </Banner>
          </div>
        )}
        {testPage.isSuccess ? (
          <p className="mt-3 text-sm font-medium text-emerald-700 dark:text-emerald-300">
            Test page sent. Every color bar should be solid.
          </p>
        ) : (
          <Button
            className={cx("mt-3 w-full", !s.nudge && "min-h-11 text-sm")}
            variant={s.nudge ? "primary" : "secondary"}
            busy={testPage.isPending}
            onClick={() => testPage.mutate()}
          >
            Print a test page
          </Button>
        )}
        {testPage.error && (
          <p className="mt-2 text-sm text-rose-600">{testPage.error.message}</p>
        )}
      </div>
    </Card>
  );
}

function lastPrinted(s: Supplies) {
  if (s.idle_days < 0 || !s.last_printed) return "No prints recorded yet.";
  if (s.idle_days === 0) return "Last printed today.";
  if (s.idle_days === 1) return "Last printed yesterday.";
  return `Last printed ${s.idle_days} days ago.`;
}

function age(days: number) {
  if (days < 0) return "Install date not set";
  if (days === 0) return "Installed today";
  if (days < 45) return `Installed ${days} ${days === 1 ? "day" : "days"} ago`;
  const months = Math.round(days / 30.4);
  if (months < 18) return `Installed ${months} months ago`;
  return `Installed ${(days / 365).toFixed(1)} years ago`;
}

function LevelBar({
  level,
  low,
  color,
}: {
  level: number;
  low: boolean;
  color: string;
}) {
  // Tri-color cartridges report one color per ink (#39D2E7#D945DD#DFD31D).
  const colors = color.match(/#[0-9a-fA-F]{6}/g) ?? ["#64748b"];
  const bg =
    colors.length > 1
      ? `linear-gradient(90deg, ${colors.join(", ")})`
      : colors[0];
  return (
    <div className="mt-2">
      <div className="flex justify-between text-sm">
        <span
          className={cx(
            low
              ? "font-semibold text-rose-700 dark:text-rose-300"
              : "text-slate-500 dark:text-slate-400",
          )}
        >
          {low ? "Low: get a replacement" : "Ink level"}
        </span>
        <span className="text-slate-500 tabular-nums dark:text-slate-400">
          {level}%
        </span>
      </div>
      <div
        className="mt-1 h-2.5 overflow-hidden rounded-full bg-slate-100 dark:bg-slate-800"
        role="meter"
        aria-valuenow={level}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label="Ink level"
      >
        <div
          className="h-full rounded-full"
          style={{ width: `${level}%`, background: bg }}
        />
      </div>
    </div>
  );
}

function CartridgeRow({
  cartridge: c,
  warnMonths,
}: {
  cartridge: Supplies["cartridges"][number];
  warnMonths: number;
}) {
  const qc = useQueryClient();
  const [editing, setEditing] = useState(false);
  const [date, setDate] = useState(c.installed_on);
  const save = (d: Supplies) => {
    qc.setQueryData(["supplies"], d);
    setEditing(false);
  };
  const replaced = useMutation({
    mutationFn: () => api.cartridgeReplaced(c.id),
    onSuccess: save,
  });
  const setDateM = useMutation({
    mutationFn: () => api.setCartridgeDate(c.id, date),
    onSuccess: save,
  });
  const today = new Date().toLocaleDateString("en-CA"); // YYYY-MM-DD

  return (
    <li>
      <div className="flex items-start gap-3">
        <span
          aria-hidden="true"
          className={cx(
            "mt-1 size-4 shrink-0 rounded-full ring-1 ring-slate-900/10",
            c.id === "black"
              ? "bg-slate-900 dark:bg-slate-200"
              : "bg-[conic-gradient(#06b6d4,#d946ef,#facc15,#06b6d4)]",
          )}
        />
        <div className="min-w-0 flex-1">
          <p className="font-semibold">
            {c.name}{" "}
            <span className="font-normal text-slate-500 dark:text-slate-400">
              · {c.model}
            </span>
          </p>
          <p
            className={cx(
              "text-sm",
              c.old
                ? "font-medium text-amber-700 dark:text-amber-300"
                : "text-slate-500 dark:text-slate-400",
            )}
          >
            {age(c.age_days)}
            {c.old && ` · over ${warnMonths} months: it may be drying out`}
          </p>
          {c.level >= 0 && (
            <LevelBar level={c.level} low={c.low} color={c.color} />
          )}
        </div>
        <button
          type="button"
          onClick={() => {
            setDate(c.installed_on || today);
            setEditing((e) => !e);
          }}
          className="min-h-11 shrink-0 rounded-xl px-2 text-sm font-semibold text-teal-700 dark:text-teal-300"
        >
          {editing ? "Close" : "Change"}
        </button>
      </div>
      {editing && (
        <div className="mt-3 space-y-3 rounded-2xl bg-slate-50 p-3 dark:bg-slate-800/60">
          <Button
            variant="primary"
            className="w-full"
            busy={replaced.isPending}
            onClick={() => replaced.mutate()}
          >
            I replaced it today
          </Button>
          <form
            className="flex items-end gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              setDateM.mutate();
            }}
          >
            <label className="min-w-0 flex-1">
              <span className="mb-1 block text-sm font-medium text-slate-600 dark:text-slate-300">
                Or the date it was installed
              </span>
              <input
                type="date"
                value={date}
                max={today}
                onChange={(e) => setDate(e.target.value)}
                className="min-h-12 w-full rounded-2xl bg-white px-3 text-base ring-1 ring-slate-900/10 outline-none focus:ring-2 focus:ring-teal-500 dark:bg-slate-900"
              />
            </label>
            <Button type="submit" busy={setDateM.isPending} disabled={!date}>
              Save
            </Button>
          </form>
          {(replaced.error || setDateM.error) && (
            <p className="text-sm text-rose-600">
              {(replaced.error ?? setDateM.error)?.message}
            </p>
          )}
        </div>
      )}
    </li>
  );
}
