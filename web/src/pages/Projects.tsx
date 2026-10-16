import { A, useNavigate, useParams, useSearchParams } from "@solidjs/router";
import { createSignal, For, Show } from "solid-js";
import { Button } from "../components/Button";
import { EmptyState, QueryView } from "../components/Feedback";
import { TextArea, TextInput } from "../components/Form";
import { Icon } from "../components/Icon";
import { ConfirmDelete, Modal } from "../components/Modal";
import { PageHeader, Tabs } from "../components/Page";
import { resourceCount, ResourceList } from "../components/Rows";
import { api } from "../lib/api";
import { createAction, createQuery } from "../lib/query";
import type { Environment, Project, Resources } from "../lib/types";

function ProjectForm(props: { project?: Project; onClose: () => void; onSaved: (project: Project) => void }) {
  const [name, setName] = createSignal(props.project?.name ?? "");
  const [description, setDescription] = createSignal(props.project?.description ?? "");
  const save = createAction(
    () => {
      const body = { name: name(), description: description() };
      return props.project ? api.patch<Project>(`/projects/${props.project.id}`, body) : api.post<Project>("/projects", body);
    },
    { success: props.project ? "Projeto atualizado." : "Projeto criado.", onSuccess: props.onSaved },
  );

  return (
    <Modal title={props.project ? "Editar projeto" : "Novo projeto"} onClose={props.onClose}>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void save.run();
        }}
      >
        <TextInput label="Nome" required value={name()} onValue={setName} error={save.errors().name} placeholder="Loja virtual" />
        <TextArea label="Descrição" hint="Opcional." rows={3} value={description()} onValue={setDescription} />
        <div class="form-actions">
          <Button onClick={props.onClose}>Cancelar</Button>
          <Button type="submit" variant="primary" pending={save.pending()}>
            {props.project ? "Salvar" : "Criar projeto"}
          </Button>
        </div>
      </form>
    </Modal>
  );
}

export function ProjectsPage() {
  const navigate = useNavigate();
  const projects = createQuery(
    () => true,
    () => api.get<Project[]>("/projects"),
  );
  const [creating, setCreating] = createSignal(false);

  return (
    <div class="page">
      <PageHeader
        title="Projetos"
        description="Cada projeto reúne os ambientes e recursos de um produto."
        actions={
          <Button variant="primary" icon="plus" onClick={() => setCreating(true)}>
            Novo projeto
          </Button>
        }
      />
      <QueryView query={projects}>
        {(list) => (
          <Show
            when={list().length > 0}
            fallback={
              <EmptyState
                icon="folder"
                title="Nenhum projeto"
                action={
                  <Button variant="primary" icon="plus" onClick={() => setCreating(true)}>
                    Criar o primeiro projeto
                  </Button>
                }
              >
                Comece criando um projeto. Ele já vem com o ambiente “production”.
              </EmptyState>
            }
          >
            <div class="grid-cards">
              <For each={list()}>
                {(project) => (
                  <A href={`/projects/${project.id}`} class="card card-link stack-sm">
                    <span class="card-title truncate">{project.name}</span>
                    <span class="small muted">{project.description || "Sem descrição."}</span>
                    <div class="row" style={{ "margin-top": "var(--space-2)" }}>
                      <For each={project.environments}>{(environment) => <span class="tag">{environment.name}</span>}</For>
                    </div>
                  </A>
                )}
              </For>
            </div>
          </Show>
        )}
      </QueryView>
      <Show when={creating()}>
        <ProjectForm onClose={() => setCreating(false)} onSaved={(project) => navigate(`/projects/${project.id}`)} />
      </Show>
    </div>
  );
}

function EnvironmentForm(props: { projectId: string; onClose: () => void; onSaved: (environment: Environment) => void }) {
  const [name, setName] = createSignal("");
  const save = createAction(() => api.post<Environment>(`/projects/${props.projectId}/environments`, { name: name() }), {
    success: "Ambiente criado.",
    onSuccess: props.onSaved,
  });
  return (
    <Modal title="Novo ambiente" onClose={props.onClose}>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void save.run();
        }}
      >
        <TextInput
          label="Nome"
          required
          placeholder="staging"
          hint="Letras minúsculas, números e hífens."
          value={name()}
          onValue={setName}
          error={save.errors().name}
        />
        <div class="form-actions">
          <Button onClick={props.onClose}>Cancelar</Button>
          <Button type="submit" variant="primary" pending={save.pending()}>
            Criar ambiente
          </Button>
        </div>
      </form>
    </Modal>
  );
}

export function ProjectDetailPage() {
  const params = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [search, setSearch] = useSearchParams<{ env?: string }>();
  const project = createQuery(
    () => params.id,
    (id) => api.get<Project>(`/projects/${id}`),
  );
  const environmentId = () => search.env ?? project.data()?.environments[0]?.id;
  const environment = () => project.data()?.environments.find((item) => item.id === environmentId());
  const resources = createQuery(environmentId, (id) => api.get<Resources>(`/environments/${id}/resources`), { pollMs: 8000 });

  const [dialog, setDialog] = createSignal<"edit" | "delete" | "environment" | "delete-environment" | null>(null);
  const removeProject = createAction(() => api.delete(`/projects/${params.id}`), {
    success: "Projeto removido.",
    onSuccess: () => navigate("/projects"),
  });
  const removeEnvironment = createAction(() => api.delete(`/environments/${environmentId()}`), {
    success: "Ambiente removido.",
    onSuccess: () => {
      setDialog(null);
      setSearch({ env: undefined });
      void project.refetch();
    },
  });
  const newResourceHref = () => `/projects/${params.id}/new?env=${environmentId()}`;

  return (
    <div class="page">
      <QueryView query={project}>
        {(data) => (
          <>
            <PageHeader
              crumbs={[{ label: "Projetos", href: "/projects" }, { label: data().name }]}
              title={data().name}
              description={data().description || undefined}
              actions={
                <>
                  <Button onClick={() => setDialog("edit")}>Editar</Button>
                  <Button icon="trash" onClick={() => setDialog("delete")}>
                    Remover
                  </Button>
                  <A href={newResourceHref()} class="btn btn-primary">
                    <Icon name="plus" size={16} />
                    Novo recurso
                  </A>
                </>
              }
            />

            <div class="row-between">
              <div class="grow">
                <Tabs
                  tabs={data().environments.map((item) => ({ id: item.id, label: item.name }))}
                  selected={environmentId() ?? ""}
                  onSelect={(id) => setSearch({ env: id })}
                />
              </div>
              <div class="row">
                <Button small icon="plus" onClick={() => setDialog("environment")}>
                  Ambiente
                </Button>
                <Show when={data().environments.length > 1}>
                  <Button small variant="ghost" icon="trash" onClick={() => setDialog("delete-environment")}>
                    Remover ambiente
                  </Button>
                </Show>
              </div>
            </div>

            <QueryView query={resources}>
              {(items) => (
                <Show
                  when={resourceCount(items()) > 0}
                  fallback={
                    <EmptyState
                      icon="box"
                      title={`Nenhum recurso em “${environment()?.name ?? ""}”`}
                      action={
                        <A href={newResourceHref()} class="btn btn-primary">
                          <Icon name="plus" size={16} />
                          Adicionar recurso
                        </A>
                      }
                    >
                      Implante uma aplicação a partir de um repositório Git ou imagem Docker, suba um banco de dados ou
                      escolha um serviço pronto no catálogo.
                    </EmptyState>
                  }
                >
                  <ResourceList resources={items()} />
                </Show>
              )}
            </QueryView>

            <Show when={dialog() === "edit"}>
              <ProjectForm
                project={data()}
                onClose={() => setDialog(null)}
                onSaved={(saved) => {
                  project.set(saved);
                  setDialog(null);
                }}
              />
            </Show>
            <Show when={dialog() === "environment"}>
              <EnvironmentForm
                projectId={data().id}
                onClose={() => setDialog(null)}
                onSaved={(created) => {
                  setDialog(null);
                  setSearch({ env: created.id });
                  void project.refetch();
                }}
              />
            </Show>
            <Show when={dialog() === "delete"}>
              <ConfirmDelete
                title="Remover projeto"
                resourceName={data().name}
                description="O projeto e seus ambientes serão apagados. Só é possível remover projetos sem recursos."
                pending={removeProject.pending()}
                onConfirm={() => void removeProject.run()}
                onClose={() => setDialog(null)}
              />
            </Show>
            <Show when={dialog() === "delete-environment" && environment()}>
              <ConfirmDelete
                title="Remover ambiente"
                resourceName={environment()!.name}
                description="O ambiente será apagado. Só é possível remover ambientes sem recursos."
                pending={removeEnvironment.pending()}
                onConfirm={() => void removeEnvironment.run()}
                onClose={() => setDialog(null)}
              />
            </Show>
          </>
        )}
      </QueryView>
    </div>
  );
}
