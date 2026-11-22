import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, fileURL, pageURL, type ScanDoc } from "../api/client";
import { useSettings } from "../queries";
import { link } from "../router";
import {
  CloudIcon,
  DownloadIcon,
  PencilIcon,
  PlusIcon,
  PrinterIcon,
  TrashIcon,
  XIcon,
} from "./icons";
import { Banner, Button, Card, formatBytes, timeAgo } from "./ui";

// One scanned document: its pages and what can be done with it. Used right
// after scanning and from the Files list.
export function ScanView({
  doc,
  onChange,
  onDeleted,
  scanning,
}: {
  doc: ScanDoc;
  onChange: (d: ScanDoc) => void;
  onDeleted: () => void;
  scanning?: boolean;
}) {
  const qc = useQueryClient();
  const settings = useSettings();
  const [notice, setNotice] = useState<{
    tone: "success" | "error";
    text: string;
  } | null>(null);
  const [renaming, setRenaming] = useState(false);
  const [name, setName] = useState(doc.name);

  const refresh = (d: ScanDoc) => {
    onChange(d);
    void qc.invalidateQueries({ queryKey: ["scans"] });
  };
  const fail = (e: Error) => setNotice({ tone: "error", text: e.message });

  const addPage = useMutation({
    mutationFn: () => api.addPage(doc.id),
    onMutate: () => setNotice(null),
    onSuccess: refresh,
    onError: fail,
  });
  const removePage = useMutation({
    mutationFn: (page: number) => api.removePage(doc.id, page),
    onSuccess: refresh,
    onError: fail,
  });
  const send = useMutation({
    mutationFn: () => api.sendScan(doc.id),
    onMutate: () => setNotice(null),
    onSuccess: (r) => {
      refresh(r.scan);
      setNotice({ tone: "success", text: `Saved to Nextcloud: ${r.path}` });
    },
    onError: fail,
  });
  const print = useMutation({
    mutationFn: () =>
      api.printScan(doc.id, {
        copies: 1,
        color: doc.color === "gray" ? "monochrome" : "color",
      }),
    onMutate: () => setNotice(null),
    onSuccess: () => {
      setNotice({ tone: "success", text: "Sent to the printer." });
      void qc.invalidateQueries({ queryKey: ["jobs"] });
    },
    onError: fail,
  });
  const rename = useMutation({
    mutationFn: () => api.renameScan(doc.id, name),
    onSuccess: (d) => {
      refresh(d);
      setRenaming(false);
    },
    onError: fail,
  });
  const remove = useMutation({
    mutationFn: () => api.deleteScan(doc.id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["scans"] });
      onDeleted();
    },
    onError: fail,
  });

  const busy = scanning || addPage.isPending;
  const nextcloud = settings.data?.nextcloud.configured;
  const pages = Array.from({ length: doc.pages }, (_, i) => i + 1);

  return (
    <div className="space-y-4">
      <Card>
        {renaming ? (
          <form
            className="flex gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              rename.mutate();
            }}
          >
            <input
              autoFocus
              value={name}
              maxLength={120}
              onChange={(e) => setName(e.target.value)}
              aria-label="Scan name"
              className="min-h-12 min-w-0 flex-1 rounded-2xl bg-slate-100 px-4 text-base outline-none focus:ring-2 focus:ring-teal-500 dark:bg-slate-800"
            />
            <Button type="submit" variant="primary" busy={rename.isPending}>
              Save
            </Button>
          </form>
        ) : (
          <button
            type="button"
            onClick={() => {
              setName(doc.name);
              setRenaming(true);
            }}
            className="group flex w-full items-start justify-between gap-3 text-left"
          >
            <div className="min-w-0">
              <p className="truncate text-lg font-semibold">{doc.name}</p>
              <p className="text-sm text-slate-500 dark:text-slate-400">
                {doc.pages} {doc.pages === 1 ? "page" : "pages"} ·{" "}
                {doc.format.toUpperCase()} · {doc.resolution} dpi ·{" "}
                {formatBytes(doc.bytes)} · {timeAgo(doc.created_at)}
              </p>
            </div>
            <PencilIcon className="mt-1 size-5 shrink-0 text-slate-400 group-hover:text-teal-600" />
          </button>
        )}

        <ol className="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-3">
          {pages.map((n) => (
            <li key={n} className="relative">
              <a
                href={pageURL(doc.id, n, doc.updated_at)}
                target="_blank"
                rel="noreferrer"
              >
                <img
                  src={pageURL(doc.id, n, doc.updated_at)}
                  alt={`Page ${n}`}
                  loading="lazy"
                  className="aspect-[8.5/11] w-full rounded-xl bg-slate-100 object-cover object-top ring-1 ring-slate-900/10 dark:bg-slate-800"
                />
              </a>
              <span className="absolute bottom-2 left-2 rounded-full bg-slate-900/70 px-2 py-0.5 text-xs font-semibold text-white">
                {n}
              </span>
              {doc.pages > 1 && (
                <button
                  type="button"
                  aria-label={`Remove page ${n}`}
                  disabled={removePage.isPending || busy}
                  onClick={() => removePage.mutate(n)}
                  className="absolute top-2 right-2 grid size-9 place-items-center rounded-full bg-white/90 text-slate-700 shadow ring-1 ring-slate-900/10 disabled:opacity-50 dark:bg-slate-900/90 dark:text-slate-200"
                >
                  <XIcon className="size-4" />
                </button>
              )}
            </li>
          ))}
          {doc.format === "pdf" && (
            <li>
              <button
                type="button"
                onClick={() => addPage.mutate()}
                disabled={busy}
                className="flex aspect-[8.5/11] w-full flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed border-slate-300 text-sm font-semibold text-slate-600 transition hover:border-teal-500 hover:text-teal-700 disabled:opacity-60 dark:border-slate-700 dark:text-slate-300"
              >
                {busy ? (
                  <>
                    <span className="inline-block size-6 animate-spin rounded-full border-2 border-current border-r-transparent" />
                    Scanning…
                  </>
                ) : (
                  <>
                    <PlusIcon className="size-7" />
                    Add page
                  </>
                )}
              </button>
            </li>
          )}
        </ol>
        {doc.format === "pdf" && !busy && (
          <p className="mt-3 text-sm text-slate-500 dark:text-slate-400">
            Put the next page on the glass, then tap Add page.
          </p>
        )}
      </Card>

      {notice && <Banner tone={notice.tone}>{notice.text}</Banner>}

      <div className="grid gap-3">
        <a
          href={fileURL(doc.id, true)}
          download={doc.file_name}
          className="inline-flex min-h-12 items-center justify-center gap-2 rounded-2xl bg-teal-600 px-5 text-base font-semibold text-white shadow-sm transition hover:bg-teal-700 active:scale-[0.98] dark:bg-teal-500 dark:text-slate-950"
        >
          <DownloadIcon className="size-5" />
          Download {doc.format === "pdf" ? "PDF" : "JPEG"}
        </a>
        <div className="grid grid-cols-2 gap-3">
          {nextcloud ? (
            <Button
              icon={<CloudIcon className="size-5" />}
              busy={send.isPending}
              disabled={busy}
              onClick={() => send.mutate()}
            >
              Nextcloud
            </Button>
          ) : (
            <a
              {...link({ page: "settings" })}
              className="inline-flex min-h-12 items-center justify-center gap-2 rounded-2xl bg-slate-100 px-3 text-center text-sm font-semibold text-slate-600 dark:bg-slate-800 dark:text-slate-300"
            >
              <CloudIcon className="size-5 shrink-0" />
              Set up Nextcloud
            </a>
          )}
          <Button
            icon={<PrinterIcon className="size-5" />}
            busy={print.isPending}
            disabled={busy}
            onClick={() => print.mutate()}
          >
            Print
          </Button>
        </div>
        <Button
          variant="danger"
          icon={<TrashIcon className="size-5" />}
          busy={remove.isPending}
          disabled={busy}
          onClick={() => {
            if (confirm(`Delete “${doc.name}”?`)) remove.mutate();
          }}
        >
          Delete
        </Button>
      </div>
      {doc.deliveries.length > 0 && (
        <p className="px-1 text-sm text-slate-500 dark:text-slate-400">
          Sent to Nextcloud{" "}
          {timeAgo(doc.deliveries[doc.deliveries.length - 1]?.at)}.
        </p>
      )}
    </div>
  );
}
