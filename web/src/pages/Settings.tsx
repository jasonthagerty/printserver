import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import {
  api,
  type NextcloudInput,
  type Settings as SettingsData,
} from "../api/client";
import {
  CartridgeList,
  PrintHeadCard,
  lastPrinted,
} from "../components/Cartridges";
import { ChevronLeftIcon, ChevronRightIcon } from "../components/icons";
import {
  Banner,
  Button,
  Card,
  PageHeader,
  SectionTitle,
  Spinner,
  TextField,
} from "../components/ui";
import { useSettings, useStatus, useSupplies } from "../queries";
import { link, type SettingsSection } from "../router";

// Settings is a short menu, and each section is its own page, so no page
// needs much scrolling on a phone.
export function Settings({ section }: { section?: SettingsSection }) {
  switch (section) {
    case "ink":
      return (
        <SubPage title="Ink cartridges" subtitle="Levels, ages and reminders">
          <CartridgeList />
          <SupplySettings />
        </SubPage>
      );
    case "print-head":
      return (
        <SubPage title="Print head" subtitle="Test page and maintenance">
          <PrintHeadCard />
        </SubPage>
      );
    case "nextcloud":
      return (
        <SubPage title="Nextcloud" subtitle="Send scans straight to a folder">
          <WithSettings>{(s) => <NextcloudForm current={s} />}</WithSettings>
        </SubPage>
      );
    case "scans":
      return (
        <SubPage title="Scans">
          <WithSettings>{(s) => <RetentionForm current={s} />}</WithSettings>
        </SubPage>
      );
    case "devices":
      return (
        <SubPage title="Phones and computers">
          <Devices />
        </SubPage>
      );
    default:
      return <Menu />;
  }
}

function Menu() {
  const status = useStatus();
  const settings = useSettings();
  const supplies = useSupplies();
  const s = supplies.data;
  const nc = settings.data?.nextcloud;
  const attention =
    s?.cartridges.find((c) => c.low || c.old) && "A cartridge needs attention";
  return (
    <div className="space-y-4">
      <PageHeader title="Settings" />
      <MenuGroup title="Printer">
        <MenuRow
          section="ink"
          label="Ink cartridges"
          detail={
            attention ||
            s?.cartridges
              .map((c) => `${c.name} ${c.level >= 0 ? `${c.level}%` : "–"}`)
              .join(" · ")
          }
          warn={!!attention}
        />
        <MenuRow
          section="print-head"
          label="Print head"
          detail={s ? lastPrinted(s) : undefined}
          warn={s?.nudge}
        />
      </MenuGroup>
      <MenuGroup title="Scans">
        <MenuRow
          section="nextcloud"
          label="Nextcloud"
          detail={
            nc?.configured
              ? `${nc.user}, folder ${nc.folder || "Scans"}`
              : "Not set up"
          }
        />
        <MenuRow
          section="scans"
          label="Keep scans"
          detail={settings.data && `${settings.data.retention_days} days`}
        />
      </MenuGroup>
      <MenuGroup title="Help">
        <MenuRow
          section="devices"
          label="Printing from phones and computers"
          detail="iPhone, Mac, Android and Windows"
        />
      </MenuGroup>
      <p className="px-1 text-sm text-slate-500 dark:text-slate-400">
        Anyone on the home network can use this page and change these settings.
        Version {status.data?.version ?? "…"}
        {status.data?.mode === "test" && " (test mode)"}.
      </p>
    </div>
  );
}

function MenuGroup({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <Card className="py-3">
      <SectionTitle>{title}</SectionTitle>
      <ul className="-mx-2 divide-y divide-slate-100 dark:divide-slate-800">
        {children}
      </ul>
    </Card>
  );
}

function MenuRow({
  section,
  label,
  detail,
  warn,
}: {
  section: SettingsSection;
  label: string;
  detail?: ReactNode;
  warn?: boolean;
}) {
  return (
    <li>
      <a
        {...link({ page: "settings", section })}
        className="flex min-h-14 items-center gap-3 rounded-2xl px-2 py-2 active:bg-slate-50 dark:active:bg-slate-800"
      >
        <span className="min-w-0 flex-1">
          <span className="block font-semibold">{label}</span>
          {detail && (
            <span
              className={
                warn
                  ? "block truncate text-sm font-medium text-amber-700 dark:text-amber-300"
                  : "block truncate text-sm text-slate-500 dark:text-slate-400"
              }
            >
              {detail}
            </span>
          )}
        </span>
        <ChevronRightIcon className="size-5 shrink-0 text-slate-400" />
      </a>
    </li>
  );
}

function SubPage({
  title,
  subtitle,
  children,
}: {
  title: string;
  subtitle?: string;
  children: ReactNode;
}) {
  return (
    <div className="space-y-4">
      <a
        {...link({ page: "settings" })}
        className="-ml-2 inline-flex min-h-11 items-center gap-1 rounded-xl px-2 font-semibold text-teal-700 dark:text-teal-300"
      >
        <ChevronLeftIcon className="size-5" />
        Settings
      </a>
      <PageHeader title={title} subtitle={subtitle} />
      {children}
    </div>
  );
}

function WithSettings({
  children,
}: {
  children: (s: SettingsData) => ReactNode;
}) {
  const settings = useSettings();
  if (settings.isPending) return <Spinner className="text-slate-400" />;
  if (settings.error)
    return <Banner tone="error">{settings.error.message}</Banner>;
  return children(settings.data);
}

type Result = { ok: boolean; message: string } | null;

function NextcloudForm({ current }: { current: SettingsData }) {
  const qc = useQueryClient();
  const [nc, setNc] = useState<NextcloudInput>(() => ({
    url: current.nextcloud.url,
    user: current.nextcloud.user,
    folder: current.nextcloud.folder,
    password: "",
  }));
  const [result, setResult] = useState<Result>(null);

  const save = useMutation({
    mutationFn: () =>
      api.saveSettings({
        retention_days: current.retention_days,
        nextcloud: nc,
      }),
    onSuccess: (s) => {
      const lostPassword =
        current.nextcloud.password_set &&
        !s.nextcloud.password_set &&
        !nc.password;
      qc.setQueryData(["settings"], s);
      setNc((n) => ({ ...n, password: "" }));
      setResult(
        lostPassword
          ? {
              ok: false,
              message:
                "Saved. The address or user changed, so the old app password was forgotten: enter it again.",
            }
          : { ok: true, message: "Saved." },
      );
    },
    onError: (e: Error) => setResult({ ok: false, message: e.message }),
  });
  const test = useMutation({
    mutationFn: () => api.testNextcloud(nc),
    onSuccess: setResult,
    onError: (e: Error) => setResult({ ok: false, message: e.message }),
  });

  const set =
    (k: keyof NextcloudInput) => (e: React.ChangeEvent<HTMLInputElement>) => {
      setResult(null);
      setNc((n) => ({ ...n, [k]: e.target.value }));
    };
  const sameAccount =
    nc.url.replace(/\/+$/, "") === current.nextcloud.url &&
    nc.user === current.nextcloud.user;

  return (
    <form
      className="space-y-4"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <Card className="space-y-4">
        <p className="text-sm text-slate-500 dark:text-slate-400">
          Create an app password in Nextcloud under Personal settings, Security.
        </p>
        <TextField
          label="Address"
          type="url"
          inputMode="url"
          autoCapitalize="none"
          autoCorrect="off"
          placeholder="https://cloud.example.com"
          value={nc.url}
          onChange={set("url")}
        />
        <TextField
          label="User"
          autoCapitalize="none"
          autoCorrect="off"
          autoComplete="username"
          value={nc.user}
          onChange={set("user")}
        />
        <TextField
          label="App password"
          type="password"
          autoComplete="new-password"
          placeholder={
            current.nextcloud.password_set && sameAccount
              ? "Saved (leave blank to keep)"
              : ""
          }
          value={nc.password}
          onChange={set("password")}
        />
        <TextField
          label="Folder"
          placeholder="Scans"
          value={nc.folder}
          onChange={set("folder")}
        />
      </Card>
      {result && (
        <Banner tone={result.ok ? "success" : "error"}>{result.message}</Banner>
      )}
      <div className="grid grid-cols-2 gap-3">
        <Button
          busy={test.isPending}
          onClick={() => test.mutate()}
          disabled={!nc.url || !nc.user}
        >
          Test connection
        </Button>
        <Button type="submit" variant="primary" busy={save.isPending}>
          Save
        </Button>
      </div>
    </form>
  );
}

function RetentionForm({ current }: { current: SettingsData }) {
  const qc = useQueryClient();
  const [retention, setRetention] = useState(String(current.retention_days));
  const save = useMutation({
    // The password is write-only: an empty one keeps the saved one.
    mutationFn: () =>
      api.saveSettings({
        retention_days: Number(retention),
        nextcloud: {
          url: current.nextcloud.url,
          user: current.nextcloud.user,
          folder: current.nextcloud.folder,
          password: "",
        },
      }),
    onSuccess: (s) => qc.setQueryData(["settings"], s),
  });
  return (
    <form
      className="space-y-4"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <Card>
        <TextField
          label="Keep scans for (days)"
          type="number"
          inputMode="numeric"
          min={1}
          max={3650}
          value={retention}
          onChange={(e) => setRetention(e.target.value)}
          hint="Older scans are deleted from the print server."
        />
      </Card>
      {save.error && <Banner tone="error">{save.error.message}</Banner>}
      {save.isSuccess && <Banner tone="success">Saved.</Banner>}
      <Button
        type="submit"
        variant="primary"
        className="w-full"
        busy={save.isPending}
      >
        Save
      </Button>
    </form>
  );
}

function Devices() {
  const status = useStatus();
  return (
    <Card>
      {status.data?.public_url && (
        <p className="mb-3 text-slate-600 dark:text-slate-300">
          This page is at{" "}
          <a
            href={status.data.public_url}
            className="font-semibold break-all text-teal-700 dark:text-teal-300"
          >
            {status.data.public_url.replace(/\/$/, "")}
          </a>{" "}
          on any device at home.
        </p>
      )}
      <ul className="space-y-2 text-slate-600 dark:text-slate-300">
        <li>
          <b>iPhone and iPad:</b> Share, then Print. The printer appears as{" "}
          <i>{status.data?.printer?.info || "Canon MG3620"}</i>.
        </li>
        <li>
          <b>Mac:</b> it is listed in every Print dialog. Scan from Preview or
          Image Capture.
        </li>
        <li>
          <b>Android:</b> it appears through the built-in Default Print Service.
        </li>
        <li>
          <b>Windows:</b> Settings, Bluetooth &amp; devices, Printers &amp;
          scanners, Add device.
        </li>
      </ul>
    </Card>
  );
}

function SupplySettings() {
  const supplies = useSupplies();
  if (!supplies.data) return null;
  return (
    <SupplyForm
      key={`${supplies.data.warn_months}-${supplies.data.nudge_days}`}
      warnMonths={supplies.data.warn_months}
      nudgeDays={supplies.data.nudge_days}
    />
  );
}

function SupplyForm({
  warnMonths,
  nudgeDays,
}: {
  warnMonths: number;
  nudgeDays: number;
}) {
  const qc = useQueryClient();
  const [warn, setWarn] = useState(String(warnMonths));
  const [nudge, setNudge] = useState(String(nudgeDays));
  const save = useMutation({
    mutationFn: () => api.setSupplyThresholds(Number(warn), Number(nudge)),
    onSuccess: (s) => qc.setQueryData(["supplies"], s),
  });
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <Card className="space-y-4">
        <SectionTitle>Reminders</SectionTitle>
        <TextField
          label="Warn when a cartridge is older than (months)"
          type="number"
          inputMode="numeric"
          min={1}
          max={60}
          value={warn}
          onChange={(e) => setWarn(e.target.value)}
          hint="Canon suggests using a cartridge within about 6 months of opening it."
        />
        <TextField
          label="Suggest a test page after this many days without printing"
          type="number"
          inputMode="numeric"
          min={0}
          max={365}
          value={nudge}
          onChange={(e) => setNudge(e.target.value)}
          hint="0 turns the suggestion off."
        />
        {save.error && <Banner tone="error">{save.error.message}</Banner>}
        {save.isSuccess && <Banner tone="success">Saved.</Banner>}
        <Button type="submit" busy={save.isPending} className="w-full">
          Save reminders
        </Button>
      </Card>
    </form>
  );
}
