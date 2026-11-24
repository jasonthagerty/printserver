import type { ReactNode } from "react";
import {
  FilesIcon,
  HomeIcon,
  PrinterIcon,
  ScanIcon,
  SettingsIcon,
} from "./components/icons";
import { cx } from "./components/ui";
import { FileDetail, Files } from "./pages/Files";
import { Home } from "./pages/Home";
import { Print } from "./pages/Print";
import { Scan } from "./pages/Scan";
import { Settings } from "./pages/Settings";
import { link, useRoute, type Route } from "./router";

const tabs: {
  route: Route;
  label: string;
  icon: ReactNode;
  match: Route["page"][];
}[] = [
  {
    route: { page: "home" },
    label: "Home",
    icon: <HomeIcon className="size-6" />,
    match: ["home"],
  },
  {
    route: { page: "print" },
    label: "Print",
    icon: <PrinterIcon className="size-6" />,
    match: ["print"],
  },
  {
    route: { page: "scan" },
    label: "Scan",
    icon: <ScanIcon className="size-6" />,
    match: ["scan"],
  },
  {
    route: { page: "files" },
    label: "Files",
    icon: <FilesIcon className="size-6" />,
    match: ["files", "file"],
  },
  {
    route: { page: "settings" },
    label: "Settings",
    icon: <SettingsIcon className="size-6" />,
    match: ["settings"],
  },
];

export function App() {
  const route = useRoute();
  return (
    <div className="min-h-dvh bg-slate-50 text-slate-900 dark:bg-slate-950 dark:text-slate-100">
      <main className="mx-auto max-w-xl px-4 pt-[calc(1.5rem+env(safe-area-inset-top))] pb-[calc(7rem+env(safe-area-inset-bottom))]">
        <Page route={route} />
      </main>
      <nav
        aria-label="Main"
        className="fixed inset-x-0 bottom-0 z-20 border-t border-slate-200 bg-white/90 pb-[env(safe-area-inset-bottom)] backdrop-blur dark:border-slate-800 dark:bg-slate-950/90"
      >
        <ul className="mx-auto grid max-w-xl grid-cols-5">
          {tabs.map((t) => {
            const on = t.match.includes(route.page);
            return (
              <li key={t.label}>
                <a
                  {...link(t.route)}
                  aria-current={on ? "page" : undefined}
                  className={cx(
                    "flex min-h-16 flex-col items-center justify-center gap-1 text-xs font-semibold transition",
                    on
                      ? "text-teal-600 dark:text-teal-400"
                      : "text-slate-500 dark:text-slate-400",
                  )}
                >
                  {t.icon}
                  {t.label}
                </a>
              </li>
            );
          })}
        </ul>
      </nav>
    </div>
  );
}

function Page({ route }: { route: Route }) {
  switch (route.page) {
    case "print":
      return <Print />;
    case "scan":
      return <Scan />;
    case "files":
      return <Files />;
    case "file":
      return <FileDetail id={route.id} />;
    case "settings":
      return <Settings section={route.section} />;
    default:
      return <Home />;
  }
}
