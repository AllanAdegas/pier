// Small data-fetching primitives. They are deliberately simpler than
// createResource: no Suspense, stale data stays visible while refetching, and
// polling is built in (statuses change on the server without user action).

import { createEffect, createMemo, createSignal, on, onCleanup, type Accessor } from "solid-js";
import { errorMessage, fieldErrors } from "./api";
import { notify } from "./state";

export interface Query<T> {
  data: Accessor<T | undefined>;
  error: Accessor<unknown>;
  loading: Accessor<boolean>;
  refetch: () => Promise<void>;
  /** Replaces the cached value, e.g. with the response of a mutation. */
  set: (value: T) => void;
}

export interface QueryOptions {
  /** Refetch interval in milliseconds; 0 or undefined disables polling. */
  pollMs?: number | Accessor<number>;
}

/**
 * Fetches whenever `source` changes. A falsy source skips the fetch, which
 * lets a query wait for a route parameter or a parent query.
 */
export function createQuery<S, T>(
  source: Accessor<S | false | null | undefined>,
  fetcher: (source: S) => Promise<T>,
  options: QueryOptions = {},
): Query<T> {
  const [data, setData] = createSignal<T>();
  const [error, setError] = createSignal<unknown>();
  const [loading, setLoading] = createSignal(true);
  let latestRequest = 0;

  const run = async (): Promise<void> => {
    const current = source();
    if (current === false || current === null || current === undefined) {
      return;
    }
    const request = ++latestRequest;
    try {
      const value = await fetcher(current);
      // A slower, older response must not overwrite a newer one.
      if (request === latestRequest) {
        setData(() => value);
        setError(undefined);
      }
    } catch (failure) {
      if (request === latestRequest) {
        setError(failure);
      }
    } finally {
      if (request === latestRequest) {
        setLoading(false);
      }
    }
  };

  // Memoized: the query restarts only when the source value changes, not
  // whenever a signal it reads is touched (e.g. switching between two tabs
  // that both need the same data).
  const key = createMemo(source);
  createEffect(
    on(key, () => {
      setLoading(true);
      setData(undefined);
      void run();
    }),
  );

  createEffect(() => {
    const interval = typeof options.pollMs === "function" ? options.pollMs() : options.pollMs;
    if (!interval) {
      return;
    }
    const timer = window.setInterval(() => {
      if (document.visibilityState === "visible") {
        void run();
      }
    }, interval);
    onCleanup(() => window.clearInterval(timer));
  });

  return { data, error, loading, refetch: run, set: (value) => setData(() => value) };
}

export interface Action<A extends unknown[], R> {
  run: (...args: A) => Promise<R | undefined>;
  pending: Accessor<boolean>;
  /** Validation messages keyed by field path, from the last failure. */
  errors: Accessor<Record<string, string>>;
}

export interface ActionOptions<R> {
  /** Toast shown on success. */
  success?: string | ((result: R) => string);
  onSuccess?: (result: R) => void;
}

/**
 * Wraps a mutation: tracks the pending state, shows a toast with the outcome
 * and exposes field errors so forms can highlight what to fix.
 */
export function createAction<A extends unknown[], R>(
  mutation: (...args: A) => Promise<R>,
  options: ActionOptions<R> = {},
): Action<A, R> {
  const [pending, setPending] = createSignal(false);
  const [errors, setErrors] = createSignal<Record<string, string>>({});

  const run = async (...args: A): Promise<R | undefined> => {
    if (pending()) {
      return undefined;
    }
    setPending(true);
    setErrors({});
    try {
      const result = await mutation(...args);
      if (options.success) {
        notify(typeof options.success === "function" ? options.success(result) : options.success);
      }
      options.onSuccess?.(result);
      return result;
    } catch (failure) {
      setErrors(fieldErrors(failure));
      notify(errorMessage(failure), "danger");
      return undefined;
    } finally {
      setPending(false);
    }
  };

  return { run, pending, errors };
}
