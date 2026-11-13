import { Show } from "solid-js";
import { NumberInput, Select, TextInput } from "../../components/Form";
import { Section } from "../../components/Page";
import type { ApplicationConfig } from "../../lib/types";
import { t } from "../../i18n";

export function EngineExtras(props: {
  config: ApplicationConfig;
  errors: Record<string, string>;
  onTarget: (target: string) => void;
  onRuntime: (runtime: NonNullable<ApplicationConfig["runtime"]>) => void;
}) {
  const runtime = () => props.config.runtime ?? { restart_policy: "unless-stopped", stop_grace_seconds: 0 };
  return (
    <Section title={t("Build e ciclo de vida")}>
      <Show when={props.config.build.pack === "dockerfile"}>
        <TextInput label={t("Estágio do Dockerfile")} mono value={props.config.build.target ?? ""}
          hint={t("Opcional: nome do estágio final em um Dockerfile com múltiplos estágios.")}
          onValue={props.onTarget} error={props.errors["build.target"]} />
      </Show>
      <Select label={t("Política de reinício")} value={runtime().restart_policy || "unless-stopped"}
        options={["unless-stopped", "always", "on-failure", "no"].map((value) => ({ value, label: value }))}
        onValue={(restart_policy) => props.onRuntime({ ...runtime(), restart_policy })}
        error={props.errors["runtime.restart_policy"]} />
      <NumberInput label={t("Prazo para encerrar processos (segundos)")} min={0} max={300}
        hint={t("0 mantém o padrão do Pier (30 segundos). Depois do prazo, o Docker força o encerramento.")}
        value={runtime().stop_grace_seconds}
        onValue={(stop_grace_seconds) => props.onRuntime({ ...runtime(), stop_grace_seconds })}
        error={props.errors["runtime.stop_grace_seconds"]} />
    </Section>
  );
}
