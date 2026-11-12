import { createSignal, For, Show } from "solid-js";
import { Button } from "../../components/Button";
import { Alert, QueryView } from "../../components/Feedback";
import { Checkbox, Select, TextArea, TextInput } from "../../components/Form";
import { CopyField, Section } from "../../components/Page";
import { api } from "../../lib/api";
import { formatRelative } from "../../lib/format";
import { createAction, createQuery } from "../../lib/query";
import type { PrivateKey, Stack, StackGitSource, WebhookInfo } from "../../lib/types";
import { t } from "../../i18n";
import { RISK_LABELS } from "../new/CatalogStackForm";
import { draftFrom, inputFrom, parseLines, shortCommit, usesSsh, withAcceptedRisks } from "./gitSource";

/** What the last deployment of a Git stack ran: repository, ref and commit. */
export function GitSummary(props: { git: StackGitSource }) {
  return (
    <Section title={t("Origem Git")} description={t("O compose vem do repositório e é clonado a cada deploy.")}>
      <div class="list">
        <div class="list-item">
          <span class="muted">{t("Repositório")}</span>
          <span class="mono truncate">{props.git.repository}</span>
        </div>
        <div class="list-item">
          <span class="muted">{props.git.tag ? "Tag" : "Branch"}</span>
          <span class="mono">{props.git.tag || props.git.branch}</span>
        </div>
        <div class="list-item">
          <span class="muted">{t("Último deploy")}</span>
          <Show when={props.git.last_commit} fallback={<span class="muted">{t("Ainda não implantada.")}</span>}>
            <span class="truncate">
              <span class="mono">{shortCommit(props.git.last_commit)}</span> — {props.git.last_commit_message}
              <Show when={props.git.last_deployed_at}>
                {(at) => <span class="muted"> · {formatRelative(at())}</span>}
              </Show>
            </span>
          </Show>
        </div>
      </div>
      <PendingRisks git={props.git} />
    </Section>
  );
}

function PendingRisks(props: { git: StackGitSource }) {
  return (
    <Show when={props.git.pending_risks.length > 0}>
      <Alert tone="danger">
        {t("O último deploy foi recusado: o compose do repositório pede poderes sobre o servidor que ainda não foram aceitos. Aceite-os na aba Git para implantar.")}
        <ul>
          <For each={props.git.pending_risks}>{(risk) => <li>{RISK_LABELS[risk] ?? risk}</li>}</For>
        </ul>
      </Alert>
    </Show>
  );
}

/** Repository settings, accepted risks and webhook of a Git stack. */
export function GitSettings(props: { stack: Stack; git: StackGitSource; onSaved: (stack: Stack) => void }) {
  const [draft, setDraft] = createSignal(draftFrom(props.git));
  const change = (patch: Partial<ReturnType<typeof draft>>) => setDraft({ ...draft(), ...patch });
  const keys = createQuery(
    () => true,
    () => api.get<PrivateKey[]>("/keys"),
  );
  const save = createAction(
    (risks: string[]) =>
      api.patch<Stack>(`/stacks/${props.stack.id}`, {
        name: props.stack.name,
        description: props.stack.description,
        routes: props.stack.routes,
        git: inputFrom(draft(), risks),
      }),
    {
      success: t("Configurações salvas. Implante para aplicar."),
      onSuccess: (saved) => {
        props.onSaved(saved);
        setDraft({ ...draft(), repository: "" });
      },
    },
  );
  const errors = () => save.errors();

  return (
    <div class="stack-lg">
      <GitSummary git={props.git} />
      <form
        class="stack-lg"
        onSubmit={(event) => {
          event.preventDefault();
          void save.run(props.git.accepted_risks);
        }}
      >
        <Section title={t("Repositório")}>
          <TextInput
            label={t("Repositório")}
            mono
            placeholder={props.git.repository}
            hint={t("Deixe em branco para manter o endereço atual (ele pode conter um token).")}
            value={draft().repository}
            onValue={(repository) => change({ repository })}
            error={errors()["git.repository"]}
          />
          <Show when={usesSsh(draft().repository || props.git.repository)}>
            <Select
              label={t("Chave de deploy")}
              placeholder={t("Selecione uma chave")}
              value={draft().privateKeyId}
              onValue={(privateKeyId) => change({ privateKeyId })}
              options={(keys.data() ?? []).map((key) => ({ value: key.id, label: key.name }))}
              error={errors()["git.private_key_id"]}
            />
            <ResetHostKey stackId={props.stack.id} />
          </Show>
          <div class="grid-2">
            <TextInput label="Branch" mono value={draft().branch} onValue={(branch) => change({ branch })} error={errors()["git.branch"]} />
            <TextInput
              label="Tag"
              mono
              hint={t("Opcional. Com uma tag, ela é implantada no lugar da branch e os webhooks de push são ignorados.")}
              value={draft().tag}
              onValue={(tag) => change({ tag })}
              error={errors()["git.tag"]}
            />
          </div>
          <div class="grid-2">
            <TextInput
              label={t("Pasta base")}
              mono
              value={draft().baseDirectory}
              onValue={(baseDirectory) => change({ baseDirectory })}
              error={errors()["git.base_directory"]}
            />
            <TextInput
              label={t("Arquivo compose")}
              mono
              value={draft().composePath}
              onValue={(composePath) => change({ composePath })}
              error={errors()["git.compose_path"]}
            />
          </div>
          <TextArea
            label={t("Caminhos monitorados")}
            mono
            rows={Math.max(2, parseLines(draft().watchPaths).length + 1)}
            hint={t("Opcional, um por linha. O webhook de push só implanta se o push alterar algum destes caminhos.")}
            value={draft().watchPaths}
            onValue={(watchPaths) => change({ watchPaths })}
            error={errors()["git.watch_paths"]}
          />
        </Section>
        <AcceptedRisks git={props.git} pending={save.pending()} onAccept={(risks) => void save.run(risks)} />
        <div class="form-actions">
          <Button type="submit" variant="primary" pending={save.pending()}>{t("Salvar configurações")}
          </Button>
        </div>
      </form>
      <Webhook stack={props.stack} git={props.git} draftAutoDeploy={draft().autoDeploy} onToggle={(autoDeploy) => change({ autoDeploy })} />
    </div>
  );
}

function AcceptedRisks(props: { git: StackGitSource; pending: boolean; onAccept: (risks: string[]) => void }) {
  return (
    <Section
      title={t("Riscos aceitos")}
      description={t("Poderes sobre o servidor que o compose deste repositório pode pedir. Um push que peça mais do que isto é recusado.")}
    >
      <Show when={props.git.accepted_risks.length > 0} fallback={<p class="muted">{t("Nenhum risco aceito.")}</p>}>
        <ul class="small">
          <For each={props.git.accepted_risks}>{(risk) => <li>{RISK_LABELS[risk] ?? risk}</li>}</For>
        </ul>
      </Show>
      <PendingRisks git={props.git} />
      <Show when={props.git.pending_risks.length > 0}>
        <div>
          <Button disabled={props.pending} onClick={() => props.onAccept(withAcceptedRisks(props.git))}>{t("Aceitar os riscos pendentes")}
          </Button>
        </div>
      </Show>
    </Section>
  );
}

function Webhook(props: { stack: Stack; git: StackGitSource; draftAutoDeploy: boolean; onToggle: (value: boolean) => void }) {
  const webhook = createQuery(
    () => props.stack.id,
    (id) => api.get<WebhookInfo>(`/stacks/${id}/webhook`),
  );
  const rotate = createAction(() => api.post<WebhookInfo>(`/stacks/${props.stack.id}/webhook/rotate`), {
    success: t("Novo segredo gerado. Atualize-o no provedor Git."),
    onSuccess: (info) => webhook.set(info),
  });
  return (
    <Section
      title={t("Deploy automático")}
      description={t("Implante a cada push, ou dispare um deploy a partir do seu CI.")}
      actions={
        <Button small icon="refresh" pending={rotate.pending()} onClick={() => void rotate.run()}>{t("Gerar novo segredo")}
        </Button>
      }
    >
      <Checkbox
        label={t("Implantar a cada push na branch “{branch}”", { branch: props.git.branch })}
        hint={t("Cadastre uma das URLs abaixo como webhook de push no seu repositório, usando o segredo. Salve as configurações para aplicar.")}
        checked={props.draftAutoDeploy}
        onChange={props.onToggle}
      />
      <QueryView query={webhook}>
        {(info) => (
          <div class="stack">
            <CopyField label={t("Segredo")} value={info().secret} secret />
            <CopyField label={t("GitHub (evento push, content type application/json)")} value={info().github_url} />
            <CopyField label={t("GitLab (Push events, Secret token)")} value={info().gitlab_url} />
            <CopyField label={t("Gitea / Forgejo")} value={info().gitea_url} />
            <CopyField
              label={t("Gatilho genérico (CI)")}
              value={`curl -X POST -H "X-Pier-Token: ${info().secret}" ${info().deploy_url}`}
              secret
              hint={t("Dispara um deploy imediatamente, independente da branch.")}
            />
          </div>
        )}
      </QueryView>
    </Section>
  );
}

function ResetHostKey(props: { stackId: string }) {
  const reset = createAction(() => api.post(`/stacks/${props.stackId}/git/reset-host-key`), {
    success: t("Confiança do host Git redefinida."),
  });
  return (
    <div class="row">
      <Button small pending={reset.pending()} onClick={() => void reset.run()}>{t("Redefinir a confiança do host SSH")}
      </Button>
      <span class="small muted">{t("Use somente depois de confirmar a nova chave do host com o administrador do repositório.")}</span>
    </div>
  );
}
