import { Show } from "solid-js";
import { Alert } from "../../components/Feedback";
import { Checkbox, LinesInput, TextInput } from "../../components/Form";
import { Section } from "../../components/Page";
import { t } from "../../i18n";
import type { DockerOptions } from "../../lib/types";
import { dangerousOptions, formatLabels, formatUlimits, parseLabels, parseUlimits, withDefaults } from "./dockerOptions";

/**
 * Extra container options. The server validates every value against an
 * allowlist; the dangerous ones only pass when an instance owner allowed them.
 */
export function DockerOptionsEditor(props: {
  options: DockerOptions | undefined;
  errors: Record<string, string>;
  onChange: (options: DockerOptions) => void;
}) {
  const options = () => withDefaults(props.options);
  const update = (patch: Partial<DockerOptions>) => props.onChange({ ...options(), ...patch });
  const error = (field: string) => props.errors[`docker_options.${field}`];
  return (
    <Section title={t("Opções do Docker")} description={t("Opções extras do contêiner, aplicadas no próximo deploy.")}>
      <Show when={dangerousOptions(options()).length > 0 || props.errors.docker_options}>
        <Alert tone="warning">
          {props.errors.docker_options ?? t("Estas opções dão ao contêiner controle sobre o servidor e exigem a liberação de um proprietário: {list}.", { list: dangerousOptions(options()).join(", ") })}
        </Alert>
      </Show>
      <div class="grid-2">
        <LinesInput label={t("Hosts extras")} placeholder="api.interna:10.0.0.5" hint={t("Um por linha, nome:IP. Use host-gateway para o servidor.")}
          value={options().extra_hosts} onValue={(extra_hosts) => update({ extra_hosts })} error={error("extra_hosts")} />
        <LinesInput label={t("Labels")} placeholder="com.exemplo.time=web" hint={t("Um por linha, chave=valor. Prefixos pier., traefik. e com.docker. são reservados.")}
          value={formatLabels(options().labels)} onValue={(lines) => update({ labels: parseLabels(lines) })} error={error("labels")} />
        <LinesInput label={t("Adicionar capabilities")} placeholder="NET_ADMIN" value={options().cap_add}
          onValue={(cap_add) => update({ cap_add })} error={error("cap_add")} />
        <LinesInput label={t("Remover capabilities")} placeholder="ALL" value={options().cap_drop}
          onValue={(cap_drop) => update({ cap_drop })} error={error("cap_drop")} />
        <LinesInput label={t("Ulimits")} placeholder="nofile=1024:4096" hint={t("Um por linha, nome=flexível:rígido; -1 é ilimitado.")}
          value={formatUlimits(options().ulimits)} onValue={(lines) => update({ ulimits: parseUlimits(lines) })} error={error("ulimits")} />
        <LinesInput label={t("Dispositivos")} placeholder="/dev/ttyUSB0" hint={t("Exige que a instância permita dispositivos.")}
          value={options().devices} onValue={(devices) => update({ devices })} error={error("devices")} />
      </div>
      <TextInput label={t("Memória compartilhada (shm_size)")} mono placeholder="256m" value={options().shm_size}
        onValue={(shm_size) => update({ shm_size })} error={error("shm_size")} />
      <Checkbox label={t("Modo privilegiado")} checked={options().privileged} onChange={(privileged) => update({ privileged })} />
      <Checkbox label={t("Usar a rede do servidor (network_mode: host)")} hint={error("network_mode") ?? t("Sem domínios nem portas publicadas.")}
        checked={options().network_mode === "host"} onChange={(host) => update({ network_mode: host ? "host" : "" })} />
      <Checkbox label={t("Ver os processos do servidor (pid: host)")} checked={options().pid_mode === "host"}
        onChange={(host) => update({ pid_mode: host ? "host" : "" })} />
    </Section>
  );
}
