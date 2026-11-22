import { useEffect, useState } from "react";

// A tiny history-API router: five top-level pages and scan details are all
// the GUI needs.
export type Route =
  | { page: "home" }
  | { page: "print" }
  | { page: "scan" }
  | { page: "files" }
  | { page: "file"; id: string }
  | { page: "settings" };

export function parse(path: string): Route {
  const p = path.replace(/\/+$/, "") || "/";
  if (p === "/print") return { page: "print" };
  if (p === "/scan") return { page: "scan" };
  if (p === "/files") return { page: "files" };
  if (p === "/settings") return { page: "settings" };
  const m = /^\/files\/([0-9]{8}-[0-9]{6}-[0-9a-f]{6})$/.exec(p);
  if (m?.[1]) return { page: "file", id: m[1] };
  return { page: "home" };
}

export function href(r: Route): string {
  switch (r.page) {
    case "home":
      return "/";
    case "file":
      return `/files/${r.id}`;
    default:
      return `/${r.page}`;
  }
}

const listeners = new Set<() => void>();

export function navigate(r: Route | string, replace = false) {
  const to = typeof r === "string" ? r : href(r);
  if (to === location.pathname) return;
  if (replace) history.replaceState(null, "", to);
  else history.pushState(null, "", to);
  window.scrollTo(0, 0);
  listeners.forEach((l) => l());
}

export function useRoute(): Route {
  const [route, setRoute] = useState(() => parse(location.pathname));
  useEffect(() => {
    const update = () => setRoute(parse(location.pathname));
    listeners.add(update);
    window.addEventListener("popstate", update);
    return () => {
      listeners.delete(update);
      window.removeEventListener("popstate", update);
    };
  }, []);
  return route;
}

// Link props for an anchor that navigates without a page load.
export function link(r: Route) {
  const to = href(r);
  return {
    href: to,
    onClick: (e: React.MouseEvent) => {
      if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
      e.preventDefault();
      navigate(to);
    },
  };
}
