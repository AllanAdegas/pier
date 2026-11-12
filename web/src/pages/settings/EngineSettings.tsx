import { createSignal, Show } from "solid-js";
import { Button } from "../../components/Button";
import { Alert, QueryView } from "../../components/Feedback";
import { Checkbox, TextInput } from "../../components/Form";
import { Section } from "../../components/Page";
import { t } from "../../i18n";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import type { AutoDomainSettings, DockerOptionsPolicy } from "../../lib/types";

/** Instance settings of the deployment engine: automatic domains and Docker options policy. */
export function EngineSettings() {
  const domains = createQuery(() => true, () => api.get<AutoDomainSettings>("/settings/auto-domain"));
  const policy = createQuery(() => true, () => api.get<DockerOptionsPolicy>("/settings/docker-options"));
  return (
    <>
      <QueryView query={domains}>{(data) => <AutoDomainForm settings={data()} />}</QueryView>
      <QueryView query={policy}>{(data) => <DockerPolicyForm policy={data()} />}</QueryView>
    </>
  );
}

function AutoDomainForm(props: { settings: AutoDomainSettings }) {
  const [settings, setSettings] = createSignal(props.settings);
  const update = (patch: Partial<AutoDomainSettings>) => setSettings({ ...settings(), ...patch });
  const save = createAction(() => api.patch<AutoDomainSettings>("/settings/auto-domain", settings()), {
    success: t("Configuração salva."),
    onSuccess: setSettings,
  });
  return (
    <Section title={t("Domínio automático")} description={t("Aplicações sem domínio recebem um endereço no próximo deploy. Ele aparece na configuração e pode ser removido.")}>
      <form class="stack" onSubmit={(event) => { event.preventDefault(); void save.run(); }}>
        <Checkbox label={t("Dar um domínio automático a aplicações sem domínio")} checked={settings().enabled} onChange={(enabled) => update({ enabled })} />
        <TextInput label={t("Domínio base")} mono placeholder="apps.exemplo.com" value={settings().base_domain}
          hint={t("Vazio usa <app>-<ambiente>-<id>.<IP do servidor>.sslip.io, sem configurar DNS. Com um domínio próprio, aponte *.<domínio> para os servidores.")}
          onValue={(base_domain) => update({ base_domain, https: base_domain ? settings().https : false })} error={save.errors().base_domain} />
        <Checkbox label={t("Emitir certificado HTTPS")} hint={save.errors().https ?? t("Só com domínio base próprio; endereços sslip.io usam HTTP.")}
          checked={settings().https} disabled={!settings().base_domain} onChange={(https) => update({ https })} />
        <div>
          <Button type="submit" variant="primary" pending={save.pending()}>{t("Salvar")}</Button>
        </div>
      </form>
    </Section>
  );
}

function DockerPolicyForm(props: { policy: DockerOptionsPolicy }) {
  const [policy, setPolicy] = createSignal(props.policy);
  const save = createAction((next: DockerOptionsPolicy) => api.patch<DockerOptionsPolicy>("/settings/docker-options", next), {
    success: t("Configuração salva."),
    onSuccess: setPolicy,
  });
  return (
    <Section title={t("Opções do Docker")} description={t("Só um proprietário da instância pode mudar estas permissões.")}>
      <Checkbox label={t("Permitir dispositivos do servidor")} checked={policy().allow_devices}
        onChange={(allow_devices) => void save.run({ ...policy(), allow_devices })} />
      <Checkbox label={t("Permitir opções perigosas")} hint={t("Modo privilegiado, rede e processos do host, capabilities do kernel, discos e o socket do Docker.")}
        checked={policy().allow_dangerous} onChange={(allow_dangerous) => void save.run({ ...policy(), allow_dangerous })} />
      <Show when={policy().allow_dangerous}>
        <Alert tone="danger">{t("Qualquer pessoa que edite uma aplicação pode tomar o controle do servidor.")}</Alert>
      </Show>
    </Section>
  );
}
