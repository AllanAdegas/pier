import { LanguageSwitch } from "../components/LanguageSwitch";
import { A, useLocation, type RouteSectionProps } from "@solidjs/router";
import { createEffect, createSignal, For, on, Show } from "solid-js";
import { Button, IconButton } from "../components/Button";
import { Icon, type IconName } from "../components/Icon";
import { currentUser, signOut, theme, toggleTheme } from "../lib/state";
import { Alert } from "../components/Feedback";
import { UpdateBanner } from "../components/UpdateBanner";
import { createQuery, createAction } from "../lib/query";
import { api } from "../lib/api";
import { t } from "../i18n";

interface NavItem {
  href: string;
  label: string;
  icon: IconName;
  /** Other path prefixes that belong to this section. */
  also?: string[];
}

// Seven destinations at most (Hick/Miller); resources live inside projects,
// and servers and clusters share "Infraestrutura".
const NAVIGATION: NavItem[] = [
  { href: "/", label: t("Painel"), icon: "dashboard" },
  { href: "/projects", label: t("Projetos"), icon: "folder", also: ["/applications", "/databases", "/stacks"] },
  { href: "/deployments", label: "Deploys", icon: "rocket" },
  { href: "/servers", label: t("Infraestrutura"), icon: "server", also: ["/clusters", "/swarm", "/monitoring"] },
  { href: "/security", label: t("Segurança"), icon: "shield" },
  { href: "/notifications", label: t("Notificações"), icon: "bell" },
  { href: "/settings", label: t("Configurações"), icon: "settings" },
];

function isCurrent(item: NavItem, pathname: string): boolean {
  if (item.href === "/") {
    return pathname === "/";
  }
  return [item.href, ...(item.also ?? [])].some((prefix) => pathname === prefix || pathname.startsWith(prefix + "/"));
}

/** Persistent frame: brand and navigation on the left, the page on the right. */
export function Shell(props: RouteSectionProps) {
  const location = useLocation();
  const logout = createAction(signOut);
  const backup = createQuery(() => true, () => api.get<{last_backup_at: string | null}>("/instance/backups/status"), {pollMs:30000});
  const [menuOpen, setMenuOpen] = createSignal(false);

  // Navigating closes the mobile drawer.
  createEffect(on(() => location.pathname, () => setMenuOpen(false)));

  return (
    <div class="shell">
      <div class="mobile-bar">
        <A href="/" class="brand">
          <span class="brand-mark">
            <Icon name="anchor" size={20} />
          </span>
          Pier
        </A>
        <IconButton icon="menu" label={t("Abrir menu")} onClick={() => setMenuOpen(true)} />
      </div>
      <Show when={menuOpen()}>
        <button type="button" class="sidebar-scrim" aria-label={t("Fechar menu")} onClick={() => setMenuOpen(false)} />
      </Show>

      <aside class="sidebar" data-open={menuOpen() ? "true" : "false"}>
        <A href="/" class="brand">
          <span class="brand-mark">
            <Icon name="anchor" size={20} />
          </span>
          Pier
        </A>
        <nav class="nav" aria-label={t("Seções")}>
          <For each={NAVIGATION}>
            {(item) => (
              <A
                href={item.href}
                class="nav-link"
                aria-current={isCurrent(item, location.pathname) ? "page" : undefined}
                // The router's own active class is ignored; aria-current drives the style.
                activeClass=""
                inactiveClass=""
              >
                <Icon name={item.icon} />
                {item.label}
              </A>
            )}
          </For>
        </nav>
        <div class="sidebar-footer">
          <div class="user-chip">
            <span class="small truncate">
              <strong>{currentUser()?.name}</strong>
            </span>
            <span class="small muted truncate">{currentUser()?.email}</span>
          </div>
          <Button variant="ghost" small icon={theme() === "dark" ? "sun" : "moon"} onClick={toggleTheme}>
            {theme() === "dark" ? t("Tema claro") : t("Tema escuro")}
          </Button>
          <LanguageSwitch />
          <Button variant="ghost" small icon="logout" pending={logout.pending()} onClick={() => void logout.run()}>{t("Sair")}
          </Button>
        </div>
      </aside>

      <main class="main"><UpdateBanner /><Show when={backup.data() && !backup.data()!.last_backup_at}><Alert tone="warning">{t("Faça um backup da instância para proteger o Pier e suas credenciais.")} <A href="/settings#instance-backup">{t("Configurar backup")}</A></Alert></Show>{props.children}</main>
    </div>
  );
}
