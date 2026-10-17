import { useCallback, useEffect, useState } from "react";

export type Resource<T> = {
  key: string;
  data: T | null; loading: boolean; refreshing: boolean; stale: boolean;
  error: string | null; fetchedAt: string | null; revision: number;
};

/** Keep old data visibly stale while an authoritative replacement is fetched. */
export function useResource<T>(key: string, load: (signal: AbortSignal) => Promise<T>) {
  const [revision, setRevision] = useState(0);
  const [resource, setResource] = useState<Resource<T>>({ key, data: null, loading: true, refreshing: false, stale: false, error: null, fetchedAt: null, revision: 0 });
  const refresh = useCallback(() => setRevision((current) => current + 1), []);

  useEffect(() => {
    const controller = new AbortController();
    setResource((previous) => {
      const prior = previous.key === key ? previous.data : null;
      return { key, data: prior, loading: prior === null, refreshing: prior !== null, stale: prior !== null, error: null, fetchedAt: prior === null ? null : previous.fetchedAt, revision };
    });
    load(controller.signal).then((data) => {
      if (!controller.signal.aborted) setResource({ key, data, loading: false, refreshing: false, stale: false, error: null, fetchedAt: new Date().toISOString(), revision });
    }).catch((error: unknown) => {
      if (controller.signal.aborted) return;
      setResource((previous) => ({ ...previous, loading: false, refreshing: false, stale: previous.data !== null, error: error instanceof Error ? error.message : "Could not load this resource.", revision }));
    });
    return () => controller.abort();
  }, [key, load, revision]);

  return resource.key === key
    ? { ...resource, refresh }
    : { key, data: null, loading: true, refreshing: false, stale: false, error: null, fetchedAt: null, revision, refresh };
}
