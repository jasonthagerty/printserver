import type { components } from "./schema";

type Schemas = components["schemas"];
export type Status = Schemas["StatusOutputBody"];
export type PrinterStatus = Schemas["PrinterStatus"];
export type Reason = Schemas["Reason"];
export type Job = Schemas["Job"];
export type Jobs = Schemas["JobsOutputBody"];
export type PrintOptions = Schemas["PrintOptionsOutputBody"];
export type Option = Schemas["Option"];
export type Scanner = Schemas["ScannerOutputBody"];
export type ScanDoc = Schemas["ScanDoc"];
export type ScanRequest = Schemas["ScanRequestBody"];
export type CopySettings = Schemas["CopySettings"];
export type Settings = Schemas["SettingsOutputBody"];
export type SettingsInput = Schemas["SettingsInputBody"];
export type NextcloudInput = Schemas["NextcloudInput"];
export type TestResult = Schemas["TestOutputBody"];

export class ApiError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function problem(res: Response): Promise<ApiError> {
  // Problem details carry a readable message in `detail`, and validation
  // failures list each field in `errors`.
  let detail = `${res.status} ${res.statusText}`;
  try {
    const p = (await res.json()) as {
      detail?: string;
      errors?: { message?: string; location?: string }[];
    };
    if (p.detail) detail = p.detail;
    const first = p.errors?.[0];
    if (detail === "validation failed" && first?.message) {
      detail = `${first.location ?? "request"}: ${first.message}`;
    }
  } catch {
    // Not JSON; keep the status line.
  }
  return new ApiError(res.status, detail);
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const init: RequestInit = { method, headers: { Accept: "application/json" } };
  if (body !== undefined) {
    init.headers = { ...init.headers, "Content-Type": "application/json" };
    init.body = JSON.stringify(body);
  }
  let res: Response;
  try {
    res = await fetch(`/api/v1${path}`, init);
  } catch {
    throw new ApiError(
      0,
      "Can't reach the print server. Are you on the home network?",
    );
  }
  if (!res.ok) throw await problem(res);
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  status: () => request<Status>("GET", "/status"),
  jobs: () => request<Jobs>("GET", "/jobs"),
  cancelJob: (id: number) => request<void>("DELETE", `/jobs/${id}`),
  printOptions: () => request<PrintOptions>("GET", "/print-options"),
  scanner: () => request<Scanner>("GET", "/scanner"),
  scans: () => request<{ scans: ScanDoc[] }>("GET", "/scans"),
  scan: (r: ScanRequest) => request<ScanDoc>("POST", "/scans", r),
  addPage: (id: string) => request<ScanDoc>("POST", `/scans/${id}/pages`),
  removePage: (id: string, page: number) =>
    request<ScanDoc>("DELETE", `/scans/${id}/pages/${page}`),
  renameScan: (id: string, name: string) =>
    request<ScanDoc>("PATCH", `/scans/${id}`, { name }),
  deleteScan: (id: string) => request<void>("DELETE", `/scans/${id}`),
  sendScan: (id: string) =>
    request<{ path: string; scan: ScanDoc }>("POST", `/scans/${id}/send`, {
      destination: "nextcloud",
    }),
  printScan: (id: string, s: CopySettings) =>
    request<{ id: number }>("POST", `/scans/${id}/print`, s),
  copy: (s: CopySettings) => request<{ id: number }>("POST", "/copy", s),
  settings: () => request<Settings>("GET", "/settings"),
  saveSettings: (s: SettingsInput) => request<Settings>("PUT", "/settings", s),
  testNextcloud: (n: NextcloudInput) =>
    request<TestResult>("POST", "/settings/nextcloud/test", { nextcloud: n }),
};

export const pageURL = (id: string, page: number, version: string) =>
  `/api/v1/scans/${id}/pages/${page}?v=${encodeURIComponent(version)}`;
export const fileURL = (id: string, download: boolean) =>
  `/api/v1/scans/${id}/file${download ? "?download=true" : ""}`;

export type PrintFields = {
  copies: number;
  color: string;
  sides: string;
  media: string;
  quality: string;
  scaling: string;
  pages: string;
};

// Uploads with XMLHttpRequest, because fetch cannot report upload progress
// and a phone photo over Wi-Fi takes a visible moment.
export function printFile(
  file: File,
  fields: PrintFields,
  onProgress: (fraction: number) => void,
): Promise<{ id: number }> {
  const form = new FormData();
  form.append("file", file, file.name);
  for (const [k, v] of Object.entries(fields)) {
    if (v !== "" && v !== undefined) form.append(k, String(v));
  }
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", "/api/v1/jobs");
    xhr.setRequestHeader("Accept", "application/json");
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress(e.loaded / e.total);
    };
    xhr.onerror = () =>
      reject(
        new ApiError(0, "Upload failed. Check your connection and try again."),
      );
    xhr.onload = async () => {
      const res = new Response(xhr.responseText, {
        status: xhr.status,
        statusText: xhr.statusText,
      });
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve((await res.json()) as { id: number });
      } else {
        reject(await problem(res));
      }
    };
    xhr.send(form);
  });
}
