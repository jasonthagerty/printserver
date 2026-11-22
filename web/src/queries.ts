import { useQuery } from "@tanstack/react-query";
import { api } from "./api/client";

// Status and the queue poll while the page is visible: printing is short
// and people watch it happen.
export const useStatus = () =>
  useQuery({
    queryKey: ["status"],
    queryFn: api.status,
    refetchInterval: 4000,
  });

export const useJobs = () =>
  useQuery({
    queryKey: ["jobs"],
    queryFn: api.jobs,
    refetchInterval: (q) => (q.state.data?.active.length ? 2000 : 8000),
  });

export const usePrintOptions = () =>
  useQuery({
    queryKey: ["print-options"],
    queryFn: api.printOptions,
    staleTime: 5 * 60_000,
  });

export const useScanner = () =>
  useQuery({ queryKey: ["scanner"], queryFn: api.scanner, staleTime: 60_000 });

export const useScans = () =>
  useQuery({ queryKey: ["scans"], queryFn: api.scans });

export const useSettings = () =>
  useQuery({ queryKey: ["settings"], queryFn: api.settings });
