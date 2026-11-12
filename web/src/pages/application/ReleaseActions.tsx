import { useNavigate } from "@solidjs/router";
import { createMemo, createSignal, Show } from "solid-js";
import { Button } from "../../components/Button";
import { Alert, QueryView } from "../../components/Feedback";
import { Select, TextInput } from "../../components/Form";
import { Modal } from "../../components/Modal";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import type { Application, Deployment, Project } from "../../lib/types";
import { t } from "../../i18n";
import { promotionTargets } from "./release";

/** "Deploy a tag" and "Promote to…", next to the deployment history. */
export function ReleaseActions(props: { application: Application }) {
  const [dialog, setDialog] = createSignal<"tag" | "promote" | null>(null);
  const close = () => setDialog(null);
  return (
    <div class="row">
      <Show when={props.application.config.build.pack !== "image"}>
        <Button small icon="rocket" onClick={() => setDialog("tag")}>{t("Implantar tag…")}</Button>
      </Show>
      <Button small icon="rocket" onClick={() => setDialog("promote")}>{t("Promover para…")}</Button>
      <Show when={dialog() === "tag"}>
        <DeployTagDialog application={props.application} onClose={close} />
      </Show>
      <Show when={dialog() === "promote"}>
        <PromoteDialog application={props.application} onClose={close} />
      </Show>
    </div>
  );
}

function DeployTagDialog(props: { application: Application; onClose: () => void }) {
  const navigate = useNavigate();
  const [ref, setRef] = createSignal(props.application.config.source.tag);
  const deploy = createAction(
    () => api.post<Deployment>(`/applications/${props.application.id}/deploy`, { ref: ref().trim() }),
    { success: t("Deploy enfileirado."), onSuccess: (deployment) => navigate(`/deployments/${deployment.id}`) },
  );
  return (
    <Modal title={t("Implantar tag")} onClose={props.onClose}>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void deploy.run();
        }}
      >
        <TextInput
          label={t("Tag Git")}
          mono
          placeholder="v1.2.3"
          hint={t("Implanta esta tag em vez da branch configurada, só neste deploy.")}
          value={ref()}
          onValue={setRef}
          error={deploy.errors().ref}
        />
        <div class="form-actions">
          <Button type="submit" variant="primary" icon="rocket" pending={deploy.pending()} disabled={!ref().trim()}>{t("Implantar")}</Button>
        </div>
      </form>
    </Modal>
  );
}

function PromoteDialog(props: { application: Application; onClose: () => void }) {
  const navigate = useNavigate();
  const projects = createQuery(() => true, () => api.get<Project[]>("/projects"));
  const applications = createQuery(() => true, () => api.get<Application[]>("/applications"));
  const targets = createMemo(() => promotionTargets(props.application, projects.data() ?? [], applications.data() ?? []));
  const [chosen, setChosen] = createSignal("");
  const selected = () => targets().find((target) => target.application.id === chosen()) ?? targets().find((target) => target.sameServer);
  const promote = createAction(
    () => api.post<Deployment>(`/applications/${props.application.id}/promote`, { target_application_id: selected()?.application.id }),
    { success: t("Promoção enfileirada."), onSuccess: (deployment) => navigate(`/deployments/${deployment.id}`) },
  );
  return (
    <Modal title={t("Promover para outro ambiente")} onClose={props.onClose}>
      <QueryView query={applications}>
        {() => (
          <form
            class="stack"
            onSubmit={(event) => {
              event.preventDefault();
              void promote.run();
            }}
          >
            <p class="muted">
              {t("Implanta no destino a imagem que está no ar aqui, sem novo build, com as variáveis e o comando de release do destino.")}
            </p>
            <Show when={props.application.current_image} fallback={<Alert tone="warning">{t("Faça um deploy desta aplicação antes de promover.")}</Alert>}>
              <Show when={targets().length > 0} fallback={<Alert tone="info">{t("Nenhuma aplicação em outro ambiente deste projeto.")}</Alert>}>
                <Select
                  label={t("Aplicação de destino")}
                  value={selected()?.application.id ?? ""}
                  onValue={setChosen}
                  options={targets().map((target) => ({
                    value: target.application.id,
                    label: `${target.environmentName} / ${target.application.name}${target.sameServer ? "" : ` (${t("outro servidor")})`}`,
                  }))}
                  error={promote.errors().target_application_id}
                />
                <Show when={selected() && !selected()?.sameServer}>
                  <Alert tone="warning">{t("Promover entre servidores exige um registry de imagens, que ainda não está disponível.")}</Alert>
                </Show>
                <div class="form-actions">
                  <Button type="submit" variant="primary" icon="rocket" pending={promote.pending()} disabled={!selected()?.sameServer}>{t("Promover")}</Button>
                </div>
              </Show>
            </Show>
          </form>
        )}
      </QueryView>
    </Modal>
  );
}
