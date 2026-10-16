import { useParams, useSearchParams } from "@solidjs/router";
import { createSignal, For, Match, Show, Switch } from "solid-js";
import { Button } from "../components/Button";
import { QueryView } from "../components/Feedback";
import { Icon, type IconName } from "../components/Icon";
import { PageHeader } from "../components/Page";
import { api } from "../lib/api";
import { createQuery } from "../lib/query";
import type { Project, Server } from "../lib/types";
import { ApplicationForm } from "./new/ApplicationForm";
import { DatabaseForm } from "./new/DatabaseForm";
import { CatalogStackForm } from "./new/CatalogStackForm";
import { ComposeStackForm } from "./new/ComposeStackForm";
import { DirectoryStackForm } from "./new/DirectoryStackForm";

type Kind = "git" | "image" | "database" | "template" | "compose" | "directory";

const KINDS: { kind: Kind; icon: IconName; title: string; text: string }[] = [
  { kind: "git", icon: "box", title: "Aplicação do Git", text: "Clona um repositório e constrói com Dockerfile, Nixpacks ou como site estático." },
  { kind: "image", icon: "box", title: "Imagem Docker", text: "Roda uma imagem pronta de qualquer registry, sem build." },
  { kind: "database", icon: "database", title: "Banco de dados", text: "PostgreSQL, MySQL, MariaDB, MongoDB, Redis e outros, com credenciais geradas." },
  { kind: "template", icon: "layers", title: "Serviço do catálogo", text: "Mais de 350 ferramentas prontas em um clique: n8n, Grafana, WordPress, Supabase…" },
  { kind: "compose", icon: "layers", title: "Compose próprio", text: "Cole um docker-compose.yaml e gerencie a stack pelo Pier." },
  { kind: "directory", icon: "folder", title: "Pasta do servidor", text: "Suba o compose de uma pasta da máquina. O .env e os arquivos de segredo são usados onde estão, sem cópia." },
];

/** Context every creation form needs: where the resource will live. */
export interface Placement {
  environmentId: string;
  servers: Server[];
}

/** Two-step wizard: choose what to create, then fill only what that needs. */
export function NewResourcePage() {
  const params = useParams<{ id: string }>();
  const [search] = useSearchParams<{ env?: string }>();
  const [kind, setKind] = createSignal<Kind | null>(null);
  const project = createQuery(
    () => params.id,
    (id) => api.get<Project>(`/projects/${id}`),
  );
  const servers = createQuery(
    () => true,
    () => api.get<Server[]>("/servers"),
  );
  const environment = () => {
    const environments = project.data()?.environments ?? [];
    return environments.find((item) => item.id === search.env) ?? environments[0];
  };
  const selected = () => KINDS.find((item) => item.kind === kind());

  return (
    <div class="page">
      <QueryView query={project}>
        {(data) => (
          <>
            <PageHeader
              crumbs={[
                { label: "Projetos", href: "/projects" },
                { label: data().name, href: `/projects/${data().id}?env=${environment()?.id ?? ""}` },
                { label: "Novo recurso" },
              ]}
              title={selected()?.title ?? "O que você quer implantar?"}
              description={`Ambiente: ${environment()?.name ?? ""}`}
            />
            <div class="steps" aria-live="polite">
              <Show when={kind()} fallback={<span>Passo 1 de 2 — escolha o tipo</span>}>
                <Button small variant="ghost" onClick={() => setKind(null)}>
                  ← Trocar tipo
                </Button>
                <span>Passo 2 de 2 — configure</span>
              </Show>
            </div>

            <Show when={!kind()}>
              <div class="choice-grid">
                <For each={KINDS}>
                  {(item) => (
                    <button type="button" class="choice" onClick={() => setKind(item.kind)}>
                      <span style={{ color: "var(--accent)" }}>
                        <Icon name={item.icon} size={24} />
                      </span>
                      <span class="choice-title">{item.title}</span>
                      <span class="choice-text">{item.text}</span>
                    </button>
                  )}
                </For>
              </div>
            </Show>

            <Show when={kind() && environment()}>
              <QueryView query={servers}>
                {(serverList) => {
                  const placement = (): Placement => ({ environmentId: environment()!.id, servers: serverList() });
                  return (
                    <Switch>
                      <Match when={kind() === "git"}>
                        <ApplicationForm placement={placement()} source="git" />
                      </Match>
                      <Match when={kind() === "image"}>
                        <ApplicationForm placement={placement()} source="image" />
                      </Match>
                      <Match when={kind() === "database"}>
                        <DatabaseForm placement={placement()} />
                      </Match>
                      <Match when={kind() === "template"}>
                        <CatalogStackForm placement={placement()} />
                      </Match>
                      <Match when={kind() === "compose"}>
                        <ComposeStackForm placement={placement()} />
                      </Match>
                      <Match when={kind() === "directory"}>
                        <DirectoryStackForm placement={placement()} />
                      </Match>
                    </Switch>
                  );
                }}
              </QueryView>
            </Show>
          </>
        )}
      </QueryView>
    </div>
  );
}
