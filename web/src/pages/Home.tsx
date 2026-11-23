import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type Job, type PrinterStatus, type Status } from "../api/client";
import { CopyIcon, PrinterIcon, ScanIcon, XIcon } from "../components/icons";
import {
  Banner,
  Card,
  PageHeader,
  SectionTitle,
  Spinner,
  cx,
  timeAgo,
} from "../components/ui";
import { Cartridges } from "../components/Cartridges";
import { useJobs, useStatus } from "../queries";
import { link } from "../router";

export function Home() {
  const status = useStatus();
  const jobs = useJobs();

  return (
    <div className="space-y-4">
      <PageHeader title="Printer" subtitle={subtitle(status.data)} />
      {status.error && <Banner tone="error">{status.error.message}</Banner>}
      {status.data?.printer_error && (
        <Banner tone="error">{status.data.printer_error}</Banner>
      )}
      {status.data && <PrinterCard status={status.data} />}

      <div className="grid grid-cols-3 gap-3">
        <QuickAction
          to="print"
          label="Print"
          icon={<PrinterIcon className="size-7" />}
        />
        <QuickAction
          to="scan"
          label="Scan"
          icon={<ScanIcon className="size-7" />}
        />
        <QuickAction
          to="scan"
          hash="copy"
          label="Copy"
          icon={<CopyIcon className="size-7" />}
        />
      </div>

      <Queue active={jobs.data?.active} loading={jobs.isPending} />
      <Cartridges />
      {!!jobs.data?.recent.length && <Recent jobs={jobs.data.recent} />}
    </div>
  );
}

function subtitle(s: Status | undefined) {
  if (!s?.printer) return "Canon MG3620";
  return s.printer.info || s.printer.name;
}

function stateLabel(p: PrinterStatus) {
  if (!p.connected) return { text: "Not connected", tone: "bad" as const };
  if (p.reasons.some((r) => r.severity === "error"))
    return { text: "Needs attention", tone: "bad" as const };
  if (p.state === "processing")
    return { text: "Printing", tone: "busy" as const };
  if (p.state === "stopped") return { text: "Paused", tone: "bad" as const };
  return { text: "Ready", tone: "ok" as const };
}

function PrinterCard({ status }: { status: Status }) {
  const p = status.printer;
  const scanner = status.scanner;
  return (
    <Card>
      {status.mode === "test" && (
        <div className="mb-4">
          <Banner tone="info">
            Test mode: jobs go to a file and scans come from a simulated
            scanner.
          </Banner>
        </div>
      )}
      <div className="flex items-center gap-4">
        <div className="grid size-14 shrink-0 place-items-center rounded-2xl bg-teal-50 text-teal-700 dark:bg-teal-950 dark:text-teal-300">
          <PrinterIcon className="size-8" />
        </div>
        <div className="min-w-0 flex-1">
          {p ? (
            <StateLine {...stateLabel(p)} />
          ) : (
            <StateLine text="Unavailable" tone="bad" />
          )}
          <p className="mt-0.5 text-sm text-slate-500 dark:text-slate-400">
            Scanner{" "}
            {scanner.available ? scannerState(scanner.state) : "unavailable"}
          </p>
        </div>
      </div>

      {p && p.reasons.length > 0 && (
        <ul className="mt-4 space-y-2">
          {p.reasons.map((r) => (
            <li key={r.code}>
              <Banner tone={r.severity === "error" ? "error" : "warning"}>
                {r.text}
              </Banner>
            </li>
          ))}
        </ul>
      )}

      {p && p.markers.length > 0 && (
        <div className="mt-5 space-y-3">
          {p.markers.map((m) => (
            <Ink key={m.name} name={m.name} color={m.color} level={m.level} />
          ))}
        </div>
      )}
    </Card>
  );
}

function scannerState(s: string) {
  switch (s) {
    case "Idle":
      return "ready";
    case "Processing":
      return "busy";
    default:
      return s.toLowerCase() || "ready";
  }
}

function StateLine({
  text,
  tone,
}: {
  text: string;
  tone: "ok" | "busy" | "bad";
}) {
  return (
    <p className="flex items-center gap-2 text-xl font-semibold">
      <span
        className={cx(
          "size-2.5 rounded-full",
          tone === "ok" && "bg-emerald-500",
          tone === "busy" && "animate-pulse bg-sky-500",
          tone === "bad" && "bg-rose-500",
        )}
      />
      {text}
    </p>
  );
}

function Ink({
  name,
  color,
  level,
}: {
  name: string;
  color: string;
  level: number;
}) {
  // Color cartridges report several colors in one string (#00FFFF#FF00FF...).
  const colors = color.match(/#[0-9a-fA-F]{6}/g) ?? ["#64748b"];
  const bg =
    colors.length > 1
      ? `linear-gradient(90deg, ${colors.join(", ")})`
      : colors[0];
  return (
    <div>
      <div className="mb-1 flex justify-between text-sm">
        <span className="font-medium">{name}</span>
        <span className="text-slate-500 tabular-nums dark:text-slate-400">
          {level >= 0 ? `${level}%` : "unknown"}
        </span>
      </div>
      <div className="h-2.5 overflow-hidden rounded-full bg-slate-100 dark:bg-slate-800">
        {level >= 0 && (
          <div
            className="h-full rounded-full"
            style={{ width: `${level}%`, background: bg }}
          />
        )}
      </div>
    </div>
  );
}

function QuickAction({
  to,
  hash,
  label,
  icon,
}: {
  to: "print" | "scan";
  hash?: string;
  label: string;
  icon: React.ReactNode;
}) {
  const l = link({ page: to });
  return (
    <a
      {...l}
      href={hash ? `${l.href}#${hash}` : l.href}
      onClick={(e) => {
        if (hash) sessionStorage.setItem("scan-mode", hash);
        l.onClick(e);
      }}
      className="flex flex-col items-center gap-2 rounded-3xl bg-white py-5 font-semibold text-slate-800 shadow-sm ring-1 ring-slate-900/5 transition active:scale-[0.97] dark:bg-slate-900 dark:text-slate-100 dark:ring-white/10"
    >
      <span className="text-teal-600 dark:text-teal-400">{icon}</span>
      {label}
    </a>
  );
}

function Queue({ active, loading }: { active?: Job[]; loading: boolean }) {
  return (
    <Card>
      <SectionTitle>Queue</SectionTitle>
      {loading ? (
        <Spinner className="text-slate-400" />
      ) : !active?.length ? (
        <p className="text-slate-500 dark:text-slate-400">
          Nothing waiting to print.
        </p>
      ) : (
        <ul className="divide-y divide-slate-100 dark:divide-slate-800">
          {active.map((j) => (
            <JobRow key={j.id} job={j} />
          ))}
        </ul>
      )}
    </Card>
  );
}

const stateText: Record<string, string> = {
  pending: "Waiting",
  held: "Held",
  processing: "Printing",
  stopped: "Stopped",
  canceled: "Canceled",
  aborted: "Failed",
  completed: "Done",
};

function JobRow({ job }: { job: Job }) {
  const qc = useQueryClient();
  const cancel = useMutation({
    mutationFn: () => api.cancelJob(job.id),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ["jobs"] });
      void qc.invalidateQueries({ queryKey: ["status"] });
    },
  });
  return (
    <li className="flex items-center gap-3 py-3">
      <div className="min-w-0 flex-1">
        <p className="truncate font-medium">{job.name || `Job ${job.id}`}</p>
        <p className="text-sm text-slate-500 dark:text-slate-400">
          {stateText[job.state] ?? job.state}
          {job.pages > 0 &&
            ` · ${job.pages} ${job.pages === 1 ? "page" : "pages"}`}
          {job.message && ` · ${job.message}`}
        </p>
        {cancel.error && (
          <p className="text-sm text-rose-600">{cancel.error.message}</p>
        )}
      </div>
      {job.cancelable && (
        <button
          type="button"
          onClick={() => cancel.mutate()}
          disabled={cancel.isPending}
          aria-label={`Cancel ${job.name}`}
          className="grid size-11 place-items-center rounded-full text-slate-500 hover:bg-rose-50 hover:text-rose-600 disabled:opacity-50 dark:hover:bg-rose-950"
        >
          {cancel.isPending ? <Spinner /> : <XIcon className="size-5" />}
        </button>
      )}
    </li>
  );
}

function Recent({ jobs }: { jobs: Job[] }) {
  return (
    <Card>
      <SectionTitle>Recent</SectionTitle>
      <ul className="divide-y divide-slate-100 dark:divide-slate-800">
        {jobs.slice(0, 5).map((j) => (
          <li
            key={j.id}
            className="flex items-center justify-between gap-3 py-2.5"
          >
            <span className="truncate">{j.name || `Job ${j.id}`}</span>
            <span
              className={cx(
                "shrink-0 text-sm",
                j.state === "completed"
                  ? "text-slate-500 dark:text-slate-400"
                  : "font-medium text-rose-600 dark:text-rose-400",
              )}
            >
              {stateText[j.state] ?? j.state} ·{" "}
              {timeAgo(j.completed_at ?? j.created_at)}
            </span>
          </li>
        ))}
      </ul>
    </Card>
  );
}
