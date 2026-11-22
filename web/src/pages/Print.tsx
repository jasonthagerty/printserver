import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { printFile, type PrintFields } from "../api/client";
import { UploadIcon, XIcon } from "../components/icons";
import {
  Banner,
  Button,
  Card,
  PageHeader,
  Segmented,
  Select,
  Spinner,
  Stepper,
  TextField,
  formatBytes,
} from "../components/ui";
import { usePrintOptions } from "../queries";
import { link } from "../router";

const ACCEPT =
  "application/pdf,image/jpeg,image/png,text/plain,.pdf,.jpg,.jpeg,.png,.txt";

// Empty values mean "the printer's default, or the first choice it offers".
const defaults: PrintFields = {
  copies: 1,
  color: "",
  sides: "",
  media: "",
  quality: "",
  scaling: "auto",
  pages: "",
};

// Settings start fresh for each file, so a photo's settings or a PDF's page
// range never carry over to the next one.
function fieldsFor(f: File): PrintFields {
  if (f.type.startsWith("image/")) {
    // Photos: the whole picture at best quality is what people expect.
    return { ...defaults, quality: "high", scaling: "fit", sides: "one-sided" };
  }
  return defaults;
}

function pick(want: string, options: { value: string }[], prefer: string) {
  if (want && options.some((o) => o.value === want)) return want;
  if (options.some((o) => o.value === prefer)) return prefer;
  return options[0]?.value ?? "";
}

export function Print() {
  const options = usePrintOptions();
  const qc = useQueryClient();
  const input = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [fields, setFields] = useState<PrintFields>(defaults);
  const [progress, setProgress] = useState<number | null>(null);
  const [error, setError] = useState("");
  const [done, setDone] = useState<{ id: number; name: string } | null>(null);
  const preview = usePreview(file);

  const opts = options.data;
  const media = fields.media || opts?.media_default || "";
  const color = pick(fields.color, opts?.color_modes ?? [], "color");
  const sides = pick(fields.sides, opts?.sides ?? [], "one-sided");
  const quality = pick(fields.quality, opts?.qualities ?? [], "normal");
  const isPhoto = !!file?.type.startsWith("image/");
  const isPDF =
    file?.type === "application/pdf" ||
    !!file?.name.toLowerCase().endsWith(".pdf");

  function set<K extends keyof PrintFields>(k: K, v: PrintFields[K]) {
    setFields((f) => ({ ...f, [k]: v }));
  }

  function choose(f: File | undefined) {
    setError("");
    setDone(null);
    if (!f) return;
    setFile(f);
    setFields(fieldsFor(f));
  }

  async function submit() {
    if (!file) return;
    setError("");
    setProgress(0);
    try {
      const res = await printFile(
        file,
        {
          ...fields,
          media,
          color,
          sides: isPhoto ? "one-sided" : sides,
          quality,
          // Only what applies to this kind of file.
          pages: isPDF ? fields.pages.trim() : "",
          scaling: isPhoto ? fields.scaling : "auto",
        },
        setProgress,
      );
      setDone({ id: res.id, name: file.name });
      setFile(null);
      setFields(defaults);
      if (input.current) input.current.value = "";
      void qc.invalidateQueries({ queryKey: ["jobs"] });
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setProgress(null);
    }
  }

  return (
    <div className="space-y-4">
      <PageHeader title="Print" subtitle="PDFs, photos and text files" />

      {done && (
        <Banner tone="success">
          Sent “{done.name}” to the printer.{" "}
          <a {...link({ page: "home" })} className="underline">
            See the queue
          </a>
        </Banner>
      )}

      <input
        ref={input}
        type="file"
        accept={ACCEPT}
        className="sr-only"
        id="print-file"
        onChange={(e) => choose(e.target.files?.[0])}
      />

      {!file ? (
        <label
          htmlFor="print-file"
          onDragOver={(e) => e.preventDefault()}
          onDrop={(e) => {
            e.preventDefault();
            choose(e.dataTransfer.files[0]);
          }}
          className="flex cursor-pointer flex-col items-center justify-center gap-3 rounded-3xl border-2 border-dashed border-slate-300 bg-white px-6 py-14 text-center transition hover:border-teal-500 active:scale-[0.99] dark:border-slate-700 dark:bg-slate-900"
        >
          <span className="grid size-16 place-items-center rounded-full bg-teal-50 text-teal-700 dark:bg-teal-950 dark:text-teal-300">
            <UploadIcon className="size-8" />
          </span>
          <span className="text-lg font-semibold">Choose a file</span>
          <span className="text-sm text-slate-500 dark:text-slate-400">
            From Photos, Files, or your computer
          </span>
        </label>
      ) : (
        <Card className="flex items-center gap-4">
          {preview ? (
            <img
              src={preview}
              alt=""
              className="size-16 shrink-0 rounded-xl object-cover ring-1 ring-slate-900/10"
            />
          ) : (
            <div className="grid size-16 shrink-0 place-items-center rounded-xl bg-slate-100 text-xs font-bold text-slate-500 uppercase dark:bg-slate-800">
              {file.name.split(".").pop()?.slice(0, 4)}
            </div>
          )}
          <div className="min-w-0 flex-1">
            <p className="truncate font-semibold">{file.name}</p>
            <p className="text-sm text-slate-500 dark:text-slate-400">
              {formatBytes(file.size)}
            </p>
          </div>
          <button
            type="button"
            aria-label="Remove file"
            onClick={() => {
              setFile(null);
              setFields(defaults);
              if (input.current) input.current.value = "";
            }}
            className="grid size-11 place-items-center rounded-full text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800"
          >
            <XIcon className="size-5" />
          </button>
        </Card>
      )}

      <Card className="space-y-5">
        {options.isPending ? (
          <Spinner className="text-slate-400" />
        ) : options.error ? (
          <Banner tone="error">{options.error.message}</Banner>
        ) : (
          opts && (
            <>
              <Stepper
                label="Copies"
                value={fields.copies}
                min={1}
                max={99}
                onChange={(v) => set("copies", v)}
              />
              {opts.color_modes.length > 1 && (
                <Segmented
                  label="Color"
                  value={color}
                  options={opts.color_modes}
                  onChange={(v) => set("color", v)}
                />
              )}
              {opts.sides.length > 1 && !isPhoto && (
                <Select
                  label="Sides"
                  value={sides}
                  options={opts.sides}
                  onChange={(v) => set("sides", v)}
                />
              )}
              <Select
                label="Paper"
                value={media}
                options={opts.media}
                onChange={(v) => set("media", v)}
              />
              {opts.qualities.length > 1 && (
                <Segmented
                  label="Quality"
                  value={quality}
                  options={opts.qualities}
                  onChange={(v) => set("quality", v)}
                />
              )}
              {isPhoto && (
                <Segmented
                  label="Photo size"
                  value={fields.scaling}
                  options={[
                    { value: "fit", label: "Whole photo" },
                    { value: "fill", label: "Fill page" },
                  ]}
                  onChange={(v) => set("scaling", v)}
                />
              )}
              {isPDF && (
                <TextField
                  label="Pages"
                  placeholder="All pages"
                  inputMode="numeric"
                  value={fields.pages}
                  onChange={(e) => set("pages", e.target.value)}
                  hint="For example 1-3, 5"
                />
              )}
            </>
          )
        )}
      </Card>

      {error && <Banner tone="error">{error}</Banner>}

      <div className="sticky bottom-[calc(5.5rem+env(safe-area-inset-bottom))] z-10">
        <Button
          variant="primary"
          className="w-full shadow-lg"
          disabled={!file || !opts}
          busy={progress !== null}
          onClick={() => void submit()}
        >
          {progress === null
            ? fields.copies > 1
              ? `Print ${fields.copies} copies`
              : "Print"
            : progress < 1
              ? `Uploading ${Math.round(progress * 100)}%`
              : "Sending to printer…"}
        </Button>
      </div>
    </div>
  );
}

function usePreview(file: File | null) {
  const url = useMemo(
    () =>
      file && file.type.startsWith("image/") ? URL.createObjectURL(file) : null,
    [file],
  );
  useEffect(() => {
    return () => {
      if (url) URL.revokeObjectURL(url);
    };
  }, [url]);
  return url;
}
