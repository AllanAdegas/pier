// Global client state: the session, the theme and transient toasts.

import { createSignal } from "solid-js";
import { api, handleSessionExpired } from "./api";
import type { AuthStatus, User } from "./types";
import type { Tone } from "./format";

/* ---------- Session ---------- */

export type SessionPhase = "loading" | "setup" | "anonymous" | "authenticated";

const [phase, setPhase] = createSignal<SessionPhase>("loading");
const [currentUser, setCurrentUser] = createSignal<User | null>(null);

export { phase, currentUser };

export async function refreshSession(): Promise<void> {
  const status = await api.get<AuthStatus>("/auth/status");
  setCurrentUser(status.user);
  if (status.needs_setup) {
    setPhase("setup");
  } else {
    setPhase(status.authenticated ? "authenticated" : "anonymous");
  }
}

export function signIn(user: User): void {
  setCurrentUser(user);
  setPhase("authenticated");
}

export async function signOut(): Promise<void> {
  await api.post("/auth/logout").catch(() => undefined);
  setCurrentUser(null);
  setPhase("anonymous");
}

handleSessionExpired(() => {
  if (phase() === "authenticated") {
    setCurrentUser(null);
    setPhase("anonymous");
    notify("Sua sessão expirou. Entre novamente.", "danger");
  }
});

/* ---------- Theme ---------- */

export type Theme = "dark" | "light";
const THEME_KEY = "pier.theme";

function initialTheme(): Theme {
  try {
    const saved = localStorage.getItem(THEME_KEY);
    if (saved === "dark" || saved === "light") {
      return saved;
    }
  } catch {
    // Storage may be blocked; fall through to the system preference.
  }
  return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

const [theme, setThemeSignal] = createSignal<Theme>(initialTheme());
document.documentElement.dataset.theme = theme();

export { theme };

export function toggleTheme(): void {
  const next: Theme = theme() === "dark" ? "light" : "dark";
  setThemeSignal(next);
  document.documentElement.dataset.theme = next;
  try {
    localStorage.setItem(THEME_KEY, next);
  } catch {
    // Preference simply will not persist.
  }
}

/* ---------- Toasts ---------- */

export interface Toast {
  id: number;
  message: string;
  tone: Tone;
}

const TOAST_LIFETIME_MS = 5000;
const [toasts, setToasts] = createSignal<Toast[]>([]);
let nextToastId = 1;

export { toasts };

export function notify(message: string, tone: Tone = "success"): void {
  const id = nextToastId++;
  setToasts((current) => [...current, { id, message, tone }]);
  window.setTimeout(() => dismissToast(id), tone === "danger" ? TOAST_LIFETIME_MS * 2 : TOAST_LIFETIME_MS);
}

export function dismissToast(id: number): void {
  setToasts((current) => current.filter((toast) => toast.id !== id));
}
