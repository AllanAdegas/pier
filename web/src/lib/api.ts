// Thin client for the Pier HTTP API. Every call goes through `request`, the
// single place that knows about headers, error decoding and session expiry.

const BASE = "/api/v1";

/** Error answered by the backend, with an optional message per form field. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly fields: Record<string, string> = {},
  ) {
    super(message);
    this.name = "ApiError";
  }
}

type SessionExpiredHandler = () => void;
let onSessionExpired: SessionExpiredHandler = () => {};

/** Registers what to do when the backend says the session is no longer valid. */
export function handleSessionExpired(handler: SessionExpiredHandler): void {
  onSessionExpired = handler;
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let response: Response;
  try {
    response = await fetch(BASE + path, {
      method,
      credentials: "same-origin",
      headers: {
        // Required by the backend on cookie-authenticated mutations (CSRF defense).
        "X-Pier-Client": "web",
        ...(body === undefined ? {} : { "Content-Type": "application/json" }),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new ApiError(0, "network_error", "Não foi possível contatar o Pier. Verifique sua conexão.");
  }
  if (response.status === 204) {
    return undefined as T;
  }
  const payload = await response.json().catch(() => null);
  if (!response.ok) {
    if (response.status === 401 && !path.startsWith("/auth/")) {
      onSessionExpired();
    }
    const detail = payload?.error;
    throw new ApiError(
      response.status,
      detail?.code ?? "unknown",
      detail?.message ?? `Erro inesperado (HTTP ${response.status}).`,
      detail?.fields ?? {},
    );
  }
  return payload as T;
}

export const api = {
  get: <T>(path: string) => request<T>("GET", path),
  post: <T>(path: string, body?: unknown) => request<T>("POST", path, body ?? {}),
  patch: <T>(path: string, body: unknown) => request<T>("PATCH", path, body),
  put: <T>(path: string, body: unknown) => request<T>("PUT", path, body),
  delete: <T = void>(path: string) => request<T>("DELETE", path),
};

/** Human message for any thrown value. */
export function errorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return "Algo deu errado.";
}

/** Field errors of a validation failure, or an empty object. */
export function fieldErrors(error: unknown): Record<string, string> {
  return error instanceof ApiError ? error.fields : {};
}

export interface StreamHandlers<T> {
  onMessage: (payload: T) => void;
  onDone?: (payload: unknown) => void;
  onError?: () => void;
}

/**
 * Subscribes to a Server-Sent Events endpoint. The browser reconnects on its
 * own and resumes from the last event id; the returned function unsubscribes.
 */
export function openStream<T>(path: string, event: string, handlers: StreamHandlers<T>): () => void {
  const source = new EventSource(BASE + path, { withCredentials: true });
  source.addEventListener(event, (message) => {
    handlers.onMessage(JSON.parse((message as MessageEvent).data) as T);
  });
  source.addEventListener("done", (message) => {
    source.close();
    handlers.onDone?.(JSON.parse((message as MessageEvent).data));
  });
  source.onerror = () => handlers.onError?.();
  return () => source.close();
}
