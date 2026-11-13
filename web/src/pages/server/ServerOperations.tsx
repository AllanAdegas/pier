import { createSignal, For, Show } from "solid-js";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import { formatDateTime } from "../../lib/format";
import type { CleanupNote, CleanupObject, CleanupReport, OperationsPolicy } from "../../lib/types";
import { Alert, QueryView } from "../../components/Feedback";
import { Section } from "../../components/Page";
import { Button } from "../../components/Button";
import { Checkbox, NumberInput, TextInput } from "../../components/Form";
import { ConfirmTyped } from "./ConfirmTyped";
import { countByKind } from "./serverExtras";
import { t } from "../../i18n";

/** Maintenance mode and the scheduled safe cleanup of one server. */
export function ServerOperations(props: { id: string; name: string }) {
  const base = () => `/servers/${props.id}`;
  const policy = createQuery(() => props.id, () => api.get<OperationsPolicy>(`${base()}/operations-policy`));
  const last = createQuery(() => props.id, () => api.get<{ found: boolean; report: CleanupReport }>(`${base()}/safe-cleanup`));
  const [report, setReport] = createSignal<CleanupReport>();
  const [confirming, setConfirming] = createSignal(false);
  const save = createAction((value: OperationsPolicy) => api.put<OperationsPolicy>(`${base()}/operations-policy`, value), {
    success: t("Operações do servidor salvas."),
    onSuccess: (saved) => policy.set(saved),
  });
  const preview = createAction(() => api.post<CleanupReport>(`${base()}/safe-cleanup`, { dry_run: true }), { onSuccess: setReport });
  const cleanup = createAction(
    (confirmation: string) => api.post<CleanupReport>(`${base()}/safe-cleanup`, { dry_run: false, confirmation }),
    {
      success: (result) => t("Limpeza concluída: {count} objeto(s) removido(s).", { count: result.removed.length }),
      onSuccess: (result) => {
        setReport(result);
        setConfirming(false);
      },
    },
  );
  const shown = () => report() ?? (last.data()?.found ? last.data()?.report : undefined);
  const edit = (patch: Partial<OperationsPolicy>) => policy.set({ ...policy.data()!, ...patch });

  return (
    <QueryView query={policy}>
      {(current) => (
        <>
          <Section
            title={t("Modo de manutenção")}
            description={t("Pausa novos deploys neste servidor. Containers em execução, leituras, paradas e restaurações continuam.")}
          >
            <Show when={current().maintenance}>
              <Alert tone="warning">{t("Servidor em manutenção: deploys, rollbacks e webhooks são recusados.")}</Alert>
            </Show>
            <Checkbox label={t("Pausar novos deploys neste servidor")} checked={current().maintenance} onChange={(maintenance) => edit({ maintenance })} />
            <div>
              <Button pending={save.pending()} onClick={() => void save.run(current())}>{t("Salvar")}</Button>
            </div>
          </Section>
          <Section
            title={t("Limpeza segura")}
            description={t("Remove apenas o que o Pier criou e pode recriar: containers parados do Pier sem recurso cadastrado, imagens do Pier sem tag e o cache dos builders do Pier, mais antigos que a idade mínima. Volumes e containers de terceiros nunca entram.")}
          >
            <Checkbox label={t("Agendar a limpeza segura")} checked={current().cleanup_enabled} onChange={(cleanup_enabled) => edit({ cleanup_enabled })} />
            <div class="grid-2">
              <TextInput
                label={t("Agendamento (cron)")}
                mono
                value={current().cleanup_cron}
                onValue={(cleanup_cron) => edit({ cleanup_cron })}
                hint={t("5 campos, no fuso do servidor do Pier. Padrão: domingo às 4h.")}
                error={save.errors().cleanup_cron}
              />
              <NumberInput
                label={t("Idade mínima em horas")}
                value={current().min_age_hours}
                min={24}
                max={8760}
                onValue={(min_age_hours) => edit({ min_age_hours })}
                error={save.errors().min_age_hours}
              />
            </div>
            <div class="row">
              <Button pending={save.pending()} onClick={() => void save.run(current())}>{t("Salvar")}</Button>
              <Button variant="ghost" pending={preview.pending()} onClick={() => void preview.run()}>{t("Simular limpeza")}</Button>
              <Button variant="danger" icon="trash" onClick={() => setConfirming(true)}>{t("Executar limpeza agora")}</Button>
            </div>
            <Show when={shown()}>{(value) => <CleanupResult report={value()} />}</Show>
          </Section>
          <Show when={confirming()}>
            <ConfirmTyped
              title={t("Executar limpeza segura")}
              name={props.name}
              description={t("Os objetos listados na simulação serão removidos. Volumes não são tocados.")}
              action={t("Remover objetos")}
              danger
              pending={cleanup.pending()}
              onConfirm={(typed) => void cleanup.run(typed)}
              onClose={() => setConfirming(false)}
            />
          </Show>
        </>
      )}
    </QueryView>
  );
}

function CleanupResult(props: { report: CleanupReport }) {
  const counts = () => countByKind(props.report.dry_run ? props.report.candidates : props.report.removed);
  return (
    <div class="stack-sm">
      <p class="small">
        <strong>{props.report.dry_run ? t("Simulação") : t("Limpeza")}</strong>
        {" · "}
        {formatDateTime(props.report.finished_at)}
        {" · "}
        {t("{containers} container(s), {images} imagem(ns), {caches} cache(s) de build", {
          containers: counts().container, images: counts().image, caches: counts().build_cache,
        })}
      </p>
      <ObjectTable title={props.report.dry_run ? t("Seriam removidos") : t("Removidos")} objects={props.report.dry_run ? props.report.candidates : props.report.removed} />
      <NoteTable title={t("Mantidos")} notes={props.report.kept} />
      <NoteTable title={t("Falharam")} notes={props.report.failed} />
    </div>
  );
}

function ObjectTable(props: { title: string; objects: CleanupObject[] }) {
  return (
    <Show when={props.objects.length > 0} fallback={<p class="small muted">{t("Nada a remover.")}</p>}>
      <div class="table-wrap">
        <table class="table">
          <caption class="small muted">{props.title}</caption>
          <tbody>
            <For each={props.objects}>
              {(object) => (
                <tr>
                  <td>{object.kind}</td>
                  <td class="mono truncate">{object.name || object.id}</td>
                  <td class="small muted">{object.detail}</td>
                </tr>
              )}
            </For>
          </tbody>
        </table>
      </div>
    </Show>
  );
}

function NoteTable(props: { title: string; notes: CleanupNote[] }) {
  return (
    <Show when={props.notes.length > 0}>
      <div class="table-wrap">
        <table class="table">
          <caption class="small muted">{props.title}</caption>
          <tbody>
            <For each={props.notes}>
              {(note) => (
                <tr>
                  <td>{note.kind}</td>
                  <td class="mono truncate">{note.name || note.id}</td>
                  <td class="small">{note.reason}</td>
                </tr>
              )}
            </For>
          </tbody>
        </table>
      </div>
    </Show>
  );
}
