import { createSignal, For, Show } from "solid-js";
import { Alert, EmptyState, QueryView, StatusBadge } from "./Feedback";
import { Button, IconButton } from "./Button";
import { Checkbox, NumberInput, TextInput } from "./Form";
import { Modal } from "./Modal";
import { Section } from "./Page";
import { api } from "../lib/api";
import { backupStatus, formatBytes, formatDateTime, TRIGGER_LABEL } from "../lib/format";
import { createAction, createQuery, type Query } from "../lib/query";
import type { VolumeBackup, VolumeBackupPolicy, VolumeMount, VolumeOperation, VolumeResourceKind } from "../lib/types";
import { exclusionReason, hasExternalMounts, restoreConfirmed, volumeBasePath } from "../lib/volumeBackups";
import { t } from "../i18n";

/** Backups of the persistent volumes of an application or a stack. */
export function VolumeBackups(props: { kind: VolumeResourceKind; id: string; name: string }) {
  const base = () => volumeBasePath(props.kind, props.id);
  const mounts = createQuery(() => props.id, () => api.get<VolumeMount[]>(`${base()}/volume-mounts`), { pollMs: 15000 });
  const backups = createQuery(() => props.id, () => api.get<VolumeBackup[]>(`${base()}/volume-backups`), { pollMs: 4000 });
  const operations = createQuery(() => props.id, () => api.get<VolumeOperation[]>(`${base()}/volume-operations`), { pollMs: 4000 });
  const [restoring, setRestoring] = createSignal<VolumeBackup>();

  const runNow = createAction(() => api.post(`${base()}/volume-backups`), {
    success: t("Backup de volumes iniciado."),
    onSuccess: () => void backups.refetch(),
  });
  const remove = createAction((id: string) => api.delete(`/volume-backups/${id}`), {
    success: t("Backup removido."),
    onSuccess: () => void backups.refetch(),
  });

  return (
    <div class="stack-lg">
      <Section
        title={t("Backup de volumes")}
        description={t("Arquivos .tar.gz dos volumes e dos diretórios montados, guardados no diretório de dados do Pier.")}
      >
        <MountList mounts={mounts} />
        <PolicyForm kind={props.kind} id={props.id} hasExternal={hasExternalMounts(mounts.data() ?? [])} onSaved={() => void mounts.refetch()} />
      </Section>

      <section class="stack">
        <div class="row-between">
          <h2>{t("Backups de volumes")}</h2>
          <Button variant="primary" icon="download" pending={runNow.pending()} onClick={() => void runNow.run()}>
            {t("Fazer backup agora")}
          </Button>
        </div>
        <QueryView query={backups}>
          {(list) => (
            <Show
              when={list().length > 0}
              fallback={<EmptyState icon="database" title={t("Nenhum backup de volumes")}>{t("Faça um backup manual ou ative a agenda acima.")}</EmptyState>}
            >
              <div class="list">
                <For each={list()}>
                  {(backup) => <BackupRow backup={backup} onRestore={setRestoring} onRemove={(id) => void remove.run(id)} />}
                </For>
              </div>
            </Show>
          )}
        </QueryView>
      </section>

      <Show when={(operations.data() ?? []).length > 0}>
        <Section title={t("Restaurações de volumes")}>
          <For each={operations.data() ?? []}>{(op) => <OperationRow operation={op} />}</For>
        </Section>
      </Show>

      <Show when={restoring()}>
        {(backup) => (
          <RestoreModal
            backup={backup()}
            name={props.name}
            onClose={() => setRestoring(undefined)}
            onStarted={() => {
              setRestoring(undefined);
              void operations.refetch();
            }}
          />
        )}
      </Show>
    </div>
  );
}

function MountList(props: { mounts: Query<VolumeMount[]> }) {
  return (
    <QueryView query={props.mounts}>
      {(list) => (
        <Show
          when={list().length > 0}
          fallback={<p class="muted">{t("Nenhum volume em uso. Os volumes aparecem depois do primeiro deploy.")}</p>}
        >
          <div class="list">
            <For each={list()}>
              {(mount) => (
                <div class="list-item">
                  <StatusBadge
                    status={mount.included ? { label: t("Incluído"), tone: "success" } : { label: t("Fora do backup"), tone: "neutral" }}
                  />
                  <div class="grow stack-sm" style={{ gap: "0" }}>
                    <span class="mono truncate">{mount.source}</span>
                    <span class="small muted truncate">
                      {mount.kind === "volume" ? t("Volume") : t("Diretório do servidor")} · {mount.destination}
                    </span>
                    <Show when={!mount.included}>
                      <span class="small muted">{exclusionReason(mount)}</span>
                    </Show>
                  </div>
                </div>
              )}
            </For>
          </div>
        </Show>
      )}
    </QueryView>
  );
}

function PolicyForm(props: { kind: VolumeResourceKind; id: string; hasExternal: boolean; onSaved: () => void }) {
  const base = () => volumeBasePath(props.kind, props.id);
  const policy = createQuery(() => props.id, () => api.get<VolumeBackupPolicy>(`${base()}/volume-backup-policy`));
  return (
    <QueryView query={policy}>
      {(initial) => <PolicyFields initial={initial()} base={base()} hasExternal={props.hasExternal} onSaved={props.onSaved} />}
    </QueryView>
  );
}

function PolicyFields(props: { initial: VolumeBackupPolicy; base: string; hasExternal: boolean; onSaved: () => void }) {
  const [enabled, setEnabled] = createSignal(props.initial.enabled);
  const [cron, setCron] = createSignal(props.initial.cron);
  const [retention, setRetention] = createSignal(props.initial.retention);
  const [s3, setS3] = createSignal(props.initial.s3);
  const [quiesce, setQuiesce] = createSignal(props.initial.quiesce);
  const [external, setExternal] = createSignal(props.initial.include_external);
  const save = createAction(
    () =>
      api.put<VolumeBackupPolicy>(`${props.base}/volume-backup-policy`, {
        enabled: enabled(), cron: cron(), retention: retention(), s3: s3(), quiesce: quiesce(), include_external: external(),
      }),
    { success: t("Política de backup salva."), onSuccess: props.onSaved },
  );
  return (
    <form class="stack" onSubmit={(event) => { event.preventDefault(); void save.run(); }}>
      <Checkbox label={t("Fazer backup de forma agendada")} checked={enabled()} onChange={setEnabled} />
      <div class="grid-2">
        <TextInput
          label={t("Agenda (cron)")}
          mono
          hint={t("Ex.: 0 3 * * * roda todo dia às 3h (horário do servidor do Pier).")}
          value={cron()}
          onValue={setCron}
          error={save.errors().cron}
        />
        <NumberInput
          label={t("Backups mantidos")}
          hint={t("Os mais antigos são apagados automaticamente.")}
          min={1}
          max={365}
          value={retention()}
          onValue={setRetention}
          error={save.errors().retention}
        />
      </div>
      <Checkbox label={t("Enviar cópia para S3")} checked={s3()} onChange={setS3} />
      <Checkbox
        label={t("Parar os containers durante o backup")}
        hint={t("Dados consistentes, mas a aplicação fica fora do ar enquanto o arquivo é gerado.")}
        checked={quiesce()}
        onChange={setQuiesce}
      />
      <Show when={quiesce()}>
        <Alert tone="warning">{t("Com esta opção a aplicação fica indisponível durante cada backup, inclusive nos agendados.")}</Alert>
      </Show>
      <Show when={props.hasExternal || external()}>
        <Checkbox
          label={t("Incluir diretórios fora dos dados do Pier")}
          hint={t("Por padrão, só entram volumes e diretórios dentro dos dados do Pier. Diretórios de sistema nunca entram.")}
          checked={external()}
          onChange={setExternal}
        />
      </Show>
      <div class="form-actions">
        <Button type="submit" pending={save.pending()}>{t("Salvar política")}</Button>
      </div>
    </form>
  );
}

function BackupRow(props: { backup: VolumeBackup; onRestore: (backup: VolumeBackup) => void; onRemove: (id: string) => void }) {
  const backup = () => props.backup;
  return (
    <div class="list-item">
      <StatusBadge status={backupStatus(backup().status)} />
      <div class="grow stack-sm" style={{ gap: "0" }}>
        <span>{formatDateTime(backup().started_at)} · {TRIGGER_LABEL[backup().trigger] ?? backup().trigger}</span>
        <span class="small muted truncate">
          <Show when={backup().message} fallback={formatBytes(backup().size_bytes)}>{backup().message}</Show>
        </span>
      </div>
      <Show when={backup().status === "succeeded"}>
        <Button onClick={() => props.onRestore(backup())}>{t("Restaurar backup")}</Button>
        <a class="btn btn-sm" href={`/api/v1/volume-backups/${backup().id}/download`} download="">{t("Baixar")}</a>
      </Show>
      <Show when={backup().status !== "running"}>
        <IconButton icon="trash" label={t("Remover backup")} onClick={() => props.onRemove(backup().id)} />
      </Show>
    </div>
  );
}

function OperationRow(props: { operation: VolumeOperation }) {
  return (
    <div class="stack-sm">
      <span>
        {formatDateTime(props.operation.started_at)} · {t("Restauração")} · <StatusBadge status={backupStatus(props.operation.status)} />
      </span>
      <Show when={props.operation.message}><Alert tone="danger">{props.operation.message}</Alert></Show>
      <Show when={props.operation.logs}><pre class="small">{props.operation.logs}</pre></Show>
    </div>
  );
}

function RestoreModal(props: { backup: VolumeBackup; name: string; onClose: () => void; onStarted: () => void }) {
  const [confirmation, setConfirmation] = createSignal("");
  const restore = createAction(
    () => api.post(`/volume-backups/${props.backup.id}/restore`, { confirmation: confirmation() }),
    { success: t("Restauração iniciada."), onSuccess: props.onStarted },
  );
  return (
    <Modal title={t("Restaurar backup")} onClose={props.onClose}>
      <form class="stack" onSubmit={(event) => { event.preventDefault(); void restore.run(); }}>
        <Alert tone="warning">
          {t("A restauração apaga o conteúdo atual dos volumes e o substitui pelo do backup. O recurso é parado durante a operação e iniciado ao final.")}
        </Alert>
        <TextInput
          label={t("Digite \"{name}\" para confirmar", { name: props.name })}
          value={confirmation()}
          onValue={setConfirmation}
          error={restore.errors().confirmation}
        />
        <Button type="submit" variant="danger" disabled={!restoreConfirmed(confirmation(), props.name)} pending={restore.pending()}>
          {t("Restaurar backup")}
        </Button>
      </form>
    </Modal>
  );
}
