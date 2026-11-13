// Global client state: the session, the theme and transient toasts.

import { createSignal } from "solid-js";
import { resetAccess } from "../pages/access/permissions";
import { api, handleSessionExpired } from "./api";
import type { AuthStatus, User } from "./types";
import type { Tone } from "./format";
import { t } from "../i18n";

/* ---------- Session ---------- */

export type SessionPhase = "loading" | "setup" | "anonymous" | "authenticated";

const [phase, setPhase] = createSignal<SessionPhase>("loading");
const [currentUser, setCurrentUser] = createSignal<User | null>(null);

export { phase, currentUser };

export async function refreshSession(): Promise<void> {
  const status = await api.get<AuthStatus>("/auth/status");
  if (currentUser()?.id !== status.user?.id || !status.authenticated) resetAccess();
  setCurrentUser(status.user);
  if (status.needs_setup) {
    setPhase("setup");
  } else {
    setPhase(status.authenticated ? "authenticated" : "anonymous");
  }
}

export function signIn(user: User): void {
  resetAccess();
  setCurrentUser(user);
  setPhase("authenticated");
}

export async function signOut(): Promise<void> {
  await api.post("/auth/logout");
  resetAccess();
  setCurrentUser(null);
  setPhase("anonymous");
}

handleSessionExpired(() => {
  if (phase() === "authenticated") {
    resetAccess();
    setCurrentUser(null);
    setPhase("anonymous");
    notify(t("Sua sessão expirou. Entre novamente."), "danger");
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

/** Dismissals belong to the current browser and user, never another account. */
export function readOnboardingDismissals(userID: string): string[] {
 try {const value: unknown=JSON.parse(localStorage.getItem(`pier.onboarding.${userID}`) ?? "[]");return Array.isArray(value)?value.filter((v):v is string=>typeof v==="string"):[];} catch {return [];}
}
export function saveOnboardingDismissals(userID: string,items: string[]): void {
 try {localStorage.setItem(`pier.onboarding.${userID}`,JSON.stringify(items));} catch { /* Dismissals remain in memory when storage is unavailable. */ }
}
