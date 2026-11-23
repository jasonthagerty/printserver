import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  api,
  type NextcloudInput,
  type Settings as SettingsData,
} from "../api/client";
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

export function Settings() {
  const settings = useSettings();
  const status = useStatus();
  return (
    <div className="space-y-4">
      <PageHeader title="Settings" />
      {settings.isPending ? (
        <Spinner className="text-slate-400" />
      ) : settings.error ? (
        <Banner tone="error">{settings.error.message}</Banner>
      ) : (
        <SettingsForm current={settings.data} />
      )}
      <SupplySettings />
      <Card>
        <SectionTitle>Printing from phones and computers</SectionTitle>
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
            <b>Android:</b> it appears through the built-in Default Print
            Service.
          </li>
          <li>
            <b>Windows:</b> Settings, Bluetooth &amp; devices, Printers &amp;
            scanners, Add device.
          </li>
        </ul>
      </Card>
      <p className="px-1 text-sm text-slate-500 dark:text-slate-400">
        Anyone on the home network can use this page and change these settings.
        Version {status.data?.version ?? "…"}
        {status.data?.mode === "test" && " (test mode)"}.
      </p>
    </div>
  );
}

function SettingsForm({ current }: { current: SettingsData }) {
  const qc = useQueryClient();
  const [retention, setRetention] = useState(String(current.retention_days));
  const [nc, setNc] = useState<NextcloudInput>(() => ({
    url: current.nextcloud.url,
    user: current.nextcloud.user,
    folder: current.nextcloud.folder,
    password: "",
  }));
  const [result, setResult] = useState<{ ok: boolean; message: string } | null>(
    null,
  );

  const save = useMutation({
    mutationFn: () =>
      api.saveSettings({ retention_days: Number(retention), nextcloud: nc }),
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
        <SectionTitle>Nextcloud</SectionTitle>
        <p className="-mt-2 text-sm text-slate-500 dark:text-slate-400">
          Send scans straight to a folder. Create an app password in Nextcloud
          under Personal settings, Security.
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
        <Button
          busy={test.isPending}
          onClick={() => test.mutate()}
          disabled={!nc.url || !nc.user}
        >
          Test connection
        </Button>
      </Card>

      <Card>
        <SectionTitle>Scans</SectionTitle>
        <TextField
          label="Keep scans for (days)"
          type="number"
          inputMode="numeric"
          min={1}
          max={3650}
          value={retention}
          onChange={(e) => setRetention(e.target.value)}
        />
      </Card>

      {result && (
        <Banner tone={result.ok ? "success" : "error"}>{result.message}</Banner>
      )}
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
        <SectionTitle>Ink cartridges</SectionTitle>
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
          Save cartridge reminders
        </Button>
      </Card>
    </form>
  );
}
