import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, type ScanDoc } from "../api/client";
import { CopyIcon, ScanIcon } from "../components/icons";
import { ScanView } from "../components/ScanView";
import {
  Banner,
  Button,
  Card,
  PageHeader,
  Segmented,
  Spinner,
  Stepper,
} from "../components/ui";
import { useScanner } from "../queries";
import { link } from "../router";

type Mode = "scan" | "copy";

const resolutionHints: Record<number, { label: string; hint: string }> = {
  75: { label: "75", hint: "Preview" },
  150: { label: "150", hint: "Fast" },
  300: { label: "300", hint: "Document" },
  600: { label: "600", hint: "Photo" },
};

function initialMode(): Mode {
  const m = sessionStorage.getItem("scan-mode") ?? location.hash.slice(1);
  sessionStorage.removeItem("scan-mode");
  return m === "copy" ? "copy" : "scan";
}

export function Scan() {
  const [mode, setMode] = useState<Mode>(initialMode);
  const scanner = useScanner();

  return (
    <div className="space-y-4">
      <PageHeader
        title={mode === "scan" ? "Scan" : "Copy"}
        subtitle="Place the page face down on the glass"
      />
      <Segmented
        label="Mode"
        value={mode}
        options={[
          { value: "scan", label: "Scan to file" },
          { value: "copy", label: "Make a copy" },
        ]}
        onChange={(m) => {
          setMode(m);
          history.replaceState(null, "", m === "copy" ? "/scan#copy" : "/scan");
        }}
      />
      {scanner.isPending ? (
        <Spinner className="text-slate-400" />
      ) : !scanner.data?.available ? (
        <Banner tone="error">
          The scanner isn’t available. Is the printer on and connected?
        </Banner>
      ) : mode === "scan" ? (
        <ScanToFile
          resolutions={scanner.data.resolutions}
          colors={scanner.data.colors}
        />
      ) : (
        <Copy />
      )}
    </div>
  );
}

function ScanToFile({
  resolutions,
  colors,
}: {
  resolutions: number[];
  colors: string[];
}) {
  const qc = useQueryClient();
  const [resolution, setResolution] = useState(
    resolutions.includes(300) ? 300 : (resolutions[0] ?? 300),
  );
  const [color, setColor] = useState<"color" | "gray">(
    colors.includes("color") ? "color" : "gray",
  );
  const [format, setFormat] = useState<"pdf" | "jpeg">("pdf");
  const [doc, setDoc] = useState<ScanDoc | null>(null);

  const scan = useMutation({
    mutationFn: () => api.scan({ resolution, color, format }),
    onSuccess: (d) => {
      setDoc(d);
      void qc.invalidateQueries({ queryKey: ["scans"] });
    },
  });

  if (doc) {
    return (
      <div className="space-y-4">
        <ScanView doc={doc} onChange={setDoc} onDeleted={() => setDoc(null)} />
        <Button className="w-full" variant="ghost" onClick={() => setDoc(null)}>
          Start a new scan
        </Button>
      </div>
    );
  }

  return (
    <>
      <Card className="space-y-5">
        <Segmented
          label="Color"
          value={color}
          options={colors
            .filter((c): c is "color" | "gray" => c === "color" || c === "gray")
            .map((c) => ({
              value: c,
              label: c === "color" ? "Color" : "Black and white",
            }))}
          onChange={setColor}
        />
        <Segmented
          label="Resolution (dpi)"
          value={resolution}
          options={resolutions.map((r) => ({
            value: r,
            label: resolutionHints[r]?.label ?? String(r),
            hint: resolutionHints[r]?.hint,
          }))}
          onChange={setResolution}
        />
        <Segmented
          label="File type"
          value={format}
          options={[
            { value: "pdf", label: "PDF", hint: "Several pages" },
            { value: "jpeg", label: "JPEG", hint: "One image" },
          ]}
          onChange={setFormat}
        />
      </Card>
      {scan.error && <Banner tone="error">{scan.error.message}</Banner>}
      <div className="sticky bottom-[calc(5.5rem+env(safe-area-inset-bottom))] z-10">
        <Button
          variant="primary"
          className="w-full shadow-lg"
          icon={<ScanIcon className="size-5" />}
          busy={scan.isPending}
          onClick={() => scan.mutate()}
        >
          {scan.isPending ? "Scanning…" : "Scan"}
        </Button>
      </div>
      {scan.isPending && (
        <p className="text-center text-sm text-slate-500 dark:text-slate-400">
          This can take up to a minute at high resolution.
        </p>
      )}
      <p className="px-1 text-center text-sm text-slate-500 dark:text-slate-400">
        Scans are kept on the printer for a while: see{" "}
        <a
          {...link({ page: "files" })}
          className="font-medium text-teal-700 underline dark:text-teal-300"
        >
          Files
        </a>
        .
      </p>
    </>
  );
}

function Copy() {
  const qc = useQueryClient();
  const [copies, setCopies] = useState(1);
  const [color, setColor] = useState<"color" | "monochrome">("monochrome");
  const copy = useMutation({
    mutationFn: () => api.copy({ copies, color }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["jobs"] }),
  });
  return (
    <>
      <Card className="space-y-5">
        <Stepper
          label="Copies"
          value={copies}
          min={1}
          max={99}
          onChange={setCopies}
        />
        <Segmented
          label="Color"
          value={color}
          options={[
            { value: "monochrome", label: "Black and white" },
            { value: "color", label: "Color" },
          ]}
          onChange={setColor}
        />
      </Card>
      {copy.error && <Banner tone="error">{copy.error.message}</Banner>}
      {copy.isSuccess && (
        <Banner tone="success">
          Copying.{" "}
          <a {...link({ page: "home" })} className="underline">
            See the queue
          </a>
        </Banner>
      )}
      <div className="sticky bottom-[calc(5.5rem+env(safe-area-inset-bottom))] z-10">
        <Button
          variant="primary"
          className="w-full shadow-lg"
          icon={<CopyIcon className="size-5" />}
          busy={copy.isPending}
          onClick={() => copy.mutate()}
        >
          {copy.isPending
            ? "Scanning…"
            : copies > 1
              ? `Copy ${copies} times`
              : "Copy"}
        </Button>
      </div>
    </>
  );
}
