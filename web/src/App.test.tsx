import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { App } from "./App";
import { href, parse } from "./router";

const status = {
  printer: {
    name: "MG3620",
    info: "Canon MG3620",
    model: "Canon PIXMA MG3620",
    state: "idle",
    reasons: [
      {
        code: "media-empty",
        severity: "error",
        text: "Out of paper. Load paper.",
      },
    ],
    message: "",
    accepting: true,
    connected: true,
    queued_jobs: 1,
    markers: [{ name: "Black", color: "#000000", level: 40 }],
  },
  scanner: {
    available: true,
    state: "Idle",
    model: "Canon PIXMA MG3600 Series",
  },
  mode: "usb",
  version: "test",
};

const jobs = {
  active: [
    {
      id: 7,
      name: "report.pdf",
      user: "web",
      state: "processing",
      reasons: [],
      message: "",
      created_at: new Date().toISOString(),
      completed_at: null,
      pages: 1,
      cancelable: true,
    },
  ],
  recent: [],
};

const printOptions = {
  media: [
    { value: "na_letter_8.5x11in", label: "Letter" },
    { value: "na_index-4x6_4x6in", label: "4 × 6 photo" },
  ],
  media_default: "na_letter_8.5x11in",
  sides: [
    { value: "one-sided", label: "One-sided" },
    { value: "two-sided-long-edge", label: "Two-sided (book)" },
  ],
  color_modes: [
    { value: "color", label: "Color" },
    { value: "monochrome", label: "Black and white" },
  ],
  qualities: [
    { value: "normal", label: "Normal" },
    { value: "high", label: "Best" },
  ],
  accept: ["application/pdf"],
};

const settings = {
  retention_days: 30,
  nextcloud: {
    url: "https://cloud.example.com",
    user: "me",
    folder: "Scans",
    password_set: true,
    configured: true,
  },
};

type Call = { method: string; path: string; body?: unknown };

function mockAPI() {
  const calls: Call[] = [];
  const routes: Record<string, unknown> = {
    "GET /status": status,
    "GET /jobs": jobs,
    "GET /print-options": printOptions,
    "GET /settings": settings,
    "GET /scans": { scans: [] },
    "GET /supplies": {
      cartridges: [
        {
          id: "black",
          name: "Black",
          model: "PG-245 or PG-243",
          installed_on: "2026-01-02",
          age_days: 220,
          old: true,
          level: 12,
          low: true,
          color: "#101010",
        },
        {
          id: "color",
          name: "Color",
          model: "CL-246 or CL-244",
          installed_on: "",
          age_days: -1,
          old: false,
          level: -1,
          low: false,
          color: "",
        },
      ],
      warn_months: 6,
      nudge_days: 21,
      last_printed: "2026-10-01T12:00:00Z",
      idle_days: 30,
      nudge: true,
      ink_checked: new Date().toISOString(),
    },
    "GET /scanner": {
      available: true,
      model: "x",
      resolutions: [150, 300, 600],
      colors: ["color", "gray"],
      formats: ["pdf", "jpeg"],
    },
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const path = url.replace("/api/v1", "");
      calls.push({
        method,
        path,
        body: init?.body ? JSON.parse(String(init.body)) : undefined,
      });
      if (method === "DELETE") return new Response(null, { status: 204 });
      if (method === "POST" && path === "/supplies/test-page")
        return new Response(JSON.stringify({ id: 9 }), { status: 201 });
      const body = routes[`${method} ${path}`];
      if (body === undefined) return new Response("{}", { status: 404 });
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
  return calls;
}

function renderAt(path: string) {
  history.replaceState(null, "", path);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <App />
    </QueryClientProvider>,
  );
}

afterEach(() => vi.unstubAllGlobals());

test("routes round-trip", () => {
  for (const p of [
    "/",
    "/print",
    "/scan",
    "/files",
    "/settings",
    "/files/20261122-101010-abcdef",
  ]) {
    expect(href(parse(p))).toBe(p);
  }
  expect(parse("/files/../../etc")).toEqual({ page: "home" });
  expect(parse("/print/")).toEqual({ page: "print" });
});

test("home shows state, problems, ink and the queue, and cancels a job", async () => {
  const calls = mockAPI();
  renderAt("/");
  expect(await screen.findByText("Needs attention")).toBeInTheDocument();
  expect(screen.getByText("Out of paper. Load paper.")).toBeInTheDocument();
  expect(screen.getByText("40%")).toBeInTheDocument();
  expect(await screen.findByText("report.pdf")).toBeInTheDocument();

  await userEvent.click(
    screen.getByRole("button", { name: "Cancel report.pdf" }),
  );
  await waitFor(() =>
    expect(calls).toContainEqual({
      method: "DELETE",
      path: "/jobs/7",
      body: undefined,
    }),
  );
});

test("print needs a file, and offers the printer's options", async () => {
  mockAPI();
  renderAt("/print");
  const print = await screen.findByRole("button", { name: "Print" });
  expect(print).toBeDisabled();
  expect(
    await screen.findByRole("option", { name: "4 × 6 photo" }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("option", { name: "Two-sided (book)" }),
  ).toBeInTheDocument();

  const input = document.getElementById("print-file") as HTMLInputElement;
  await userEvent.upload(
    input,
    new File(["%PDF-1.4"], "a.pdf", { type: "application/pdf" }),
  );
  expect(screen.getByText("a.pdf")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Print" })).toBeEnabled();
  // Page ranges are offered for PDFs only.
  expect(screen.getByLabelText(/Pages/)).toBeInTheDocument();
});

test("settings never shows the saved password and keeps it on save", async () => {
  const calls = mockAPI();
  renderAt("/settings");
  const pw = (await screen.findByLabelText("App password")) as HTMLInputElement;
  expect(pw.value).toBe("");
  expect(pw.placeholder).toMatch(/Saved/);
  await userEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(calls.some((c) => c.method === "PUT")).toBe(true));
  const put = calls.find((c) => c.method === "PUT");
  expect(put?.body).toMatchObject({
    retention_days: 30,
    nextcloud: { url: "https://cloud.example.com", user: "me", password: "" },
  });
});

test("scan page lists the scanner's resolutions", async () => {
  mockAPI();
  renderAt("/scan");
  expect(
    await screen.findByRole("button", { name: /^300/ }),
  ).toBeInTheDocument();
  expect(screen.getByRole("button", { name: /^Scan$/ })).toBeEnabled();
});

test("home shows cartridge ages, the idle reminder, and prints a test page", async () => {
  const calls = mockAPI();
  renderAt("/");
  expect(
    await screen.findByText(/over 6 months: it may be drying out/),
  ).toBeInTheDocument();
  expect(screen.getByText("Install date not set")).toBeInTheDocument();
  expect(screen.getByText("Low: get a replacement")).toBeInTheDocument();
  expect(screen.getByText("12%")).toBeInTheDocument();
  expect(screen.getByText(/checked just now/)).toBeInTheDocument();
  expect(screen.getByText("Last printed 30 days ago.")).toBeInTheDocument();
  await userEvent.click(
    screen.getByRole("button", { name: "Print a test page" }),
  );
  expect(await screen.findByText(/Test page sent/)).toBeInTheDocument();
  expect(calls).toContainEqual({
    method: "POST",
    path: "/supplies/test-page",
    body: undefined,
  });
});
