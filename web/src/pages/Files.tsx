import { useQueryClient } from "@tanstack/react-query";
import { pageURL, type ScanDoc } from "../api/client";
import { ChevronLeftIcon, FilesIcon } from "../components/icons";
import { ScanView } from "../components/ScanView";
import {
  Banner,
  Card,
  PageHeader,
  Spinner,
  formatBytes,
  timeAgo,
} from "../components/ui";
import { useScans, useSettings } from "../queries";
import { link, navigate } from "../router";

export function Files() {
  const scans = useScans();
  const settings = useSettings();
  const days = settings.data?.retention_days;
  return (
    <div className="space-y-4">
      <PageHeader
        title="Files"
        subtitle={
          days
            ? `Scans are kept for ${days} ${days === 1 ? "day" : "days"}`
            : "Recent scans"
        }
      />
      {scans.isPending ? (
        <Spinner className="text-slate-400" />
      ) : scans.error ? (
        <Banner tone="error">{scans.error.message}</Banner>
      ) : !scans.data.scans.length ? (
        <Card className="flex flex-col items-center gap-3 py-12 text-center">
          <FilesIcon className="size-10 text-slate-300 dark:text-slate-600" />
          <p className="text-slate-500 dark:text-slate-400">No scans yet.</p>
          <a
            {...link({ page: "scan" })}
            className="font-semibold text-teal-700 dark:text-teal-300"
          >
            Scan something
          </a>
        </Card>
      ) : (
        <ul className="space-y-3">
          {scans.data.scans.map((d) => (
            <li key={d.id}>
              <a
                {...link({ page: "file", id: d.id })}
                className="flex items-center gap-4 rounded-3xl bg-white p-3 shadow-sm ring-1 ring-slate-900/5 transition active:scale-[0.99] dark:bg-slate-900 dark:ring-white/10"
              >
                <img
                  src={pageURL(d.id, 1, d.updated_at)}
                  alt=""
                  loading="lazy"
                  className="h-20 w-16 shrink-0 rounded-lg bg-slate-100 object-cover object-top ring-1 ring-slate-900/10 dark:bg-slate-800"
                />
                <div className="min-w-0 flex-1">
                  <p className="truncate font-semibold">{d.name}</p>
                  <p className="text-sm text-slate-500 dark:text-slate-400">
                    {d.pages} {d.pages === 1 ? "page" : "pages"} ·{" "}
                    {d.format.toUpperCase()} · {formatBytes(d.bytes)}
                  </p>
                  <p className="text-sm text-slate-400 dark:text-slate-500">
                    {timeAgo(d.created_at)}
                  </p>
                </div>
              </a>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export function FileDetail({ id }: { id: string }) {
  const scans = useScans();
  const qc = useQueryClient();
  const doc = scans.data?.scans.find((d) => d.id === id);
  const update = (d: ScanDoc) =>
    qc.setQueryData<{ scans: ScanDoc[] }>(["scans"], (old) =>
      old ? { scans: old.scans.map((s) => (s.id === d.id ? d : s)) } : old,
    );
  return (
    <div className="space-y-4">
      <a
        {...link({ page: "files" })}
        className="-ml-2 inline-flex min-h-11 items-center gap-1 rounded-xl px-2 font-semibold text-teal-700 dark:text-teal-300"
      >
        <ChevronLeftIcon className="size-5" />
        Files
      </a>
      {scans.isPending ? (
        <Spinner className="text-slate-400" />
      ) : !doc ? (
        <Banner tone="warning">This scan no longer exists.</Banner>
      ) : (
        <ScanView
          doc={doc}
          onChange={update}
          onDeleted={() => navigate({ page: "files" }, true)}
        />
      )}
    </div>
  );
}
