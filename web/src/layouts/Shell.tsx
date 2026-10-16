import { A, useLocation, type RouteSectionProps } from "@solidjs/router";
import { createEffect, createSignal, For, on, Show } from "solid-js";
import { Button, IconButton } from "../components/Button";
import { Icon, type IconName } from "../components/Icon";
import { currentUser, signOut, theme, toggleTheme } from "../lib/state";

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
  { href: "/", label: "Painel", icon: "dashboard" },
  { href: "/projects", label: "Projetos", icon: "folder", also: ["/applications", "/databases", "/stacks"] },
  { href: "/deployments", label: "Deploys", icon: "rocket" },
  { href: "/servers", label: "Infraestrutura", icon: "server", also: ["/clusters"] },
  { href: "/security", label: "Segurança", icon: "shield" },
  { href: "/notifications", label: "Notificações", icon: "bell" },
  { href: "/settings", label: "Configurações", icon: "settings" },
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
        <IconButton icon="menu" label="Abrir menu" onClick={() => setMenuOpen(true)} />
      </div>
      <Show when={menuOpen()}>
        <button type="button" class="sidebar-scrim" aria-label="Fechar menu" onClick={() => setMenuOpen(false)} />
      </Show>

      <aside class="sidebar" data-open={menuOpen() ? "true" : "false"}>
        <A href="/" class="brand">
          <span class="brand-mark">
            <Icon name="anchor" size={20} />
          </span>
          Pier
        </A>
        <nav class="nav" aria-label="Seções">
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
            {theme() === "dark" ? "Tema claro" : "Tema escuro"}
          </Button>
          <Button variant="ghost" small icon="logout" onClick={() => void signOut()}>
            Sair
          </Button>
        </div>
      </aside>

      <main class="main">{props.children}</main>
    </div>
  );
}
