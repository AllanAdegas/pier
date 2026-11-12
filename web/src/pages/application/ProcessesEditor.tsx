import { For } from "solid-js";
import { Button, IconButton } from "../../components/Button";
import { NumberInput, TextInput } from "../../components/Form";
import { Section } from "../../components/Page";
import type { Process } from "../../lib/types";
import { t } from "../../i18n";
import { effectiveProcesses, newProcess, WEB_PROCESS } from "./processes";

/**
 * Procfile-style processes sharing the image, variables and release. Only
 * "web" receives domains, published ports and the health check; its replicas
 * are balanced by the proxy.
 */
export function ProcessesEditor(props: { processes: Process[] | null | undefined; onChange: (processes: Process[]) => void; error?: string }) {
  const list = () => effectiveProcesses(props.processes);
  const update = (index: number, change: Partial<Process>) =>
    props.onChange(list().map((process, position) => (position === index ? { ...process, ...change } : process)));
  const updateLimits = (index: number, change: Partial<Process["limits"]>) =>
    update(index, { limits: { ...list()[index].limits, ...change } });

  return (
    <Section
      title={t("Processos")}
      description={t("Todos usam a mesma imagem, variáveis e comando de release. Só o web recebe domínios, portas e healthcheck; suas réplicas são balanceadas pelo proxy.")}
      actions={<Button small icon="plus" onClick={() => props.onChange([...list(), newProcess(list())])}>{t("Adicionar")}</Button>}
    >
      <For each={list()}>
        {(process, index) => (
          <div class="stack-sm">
            <div class="grid-3">
              <TextInput label={t("Nome")} mono value={process.name} onValue={(name) => update(index(), { name })} />
              <TextInput
                label={t("Comando")}
                mono
                placeholder={process.name === WEB_PROCESS ? t("comando de início da aplicação") : "bundle exec sidekiq"}
                value={process.command}
                onValue={(command) => update(index(), { command })}
              />
              <NumberInput label={t("Réplicas")} min={1} max={10} value={process.replicas} onValue={(replicas) => update(index(), { replicas })} />
            </div>
            <div class="grid-3">
              <TextInput
                label="CPUs"
                mono
                placeholder={t("limite da aplicação")}
                value={process.limits.cpus}
                onValue={(cpus) => updateLimits(index(), { cpus })}
              />
              <TextInput
                label={t("Memória")}
                mono
                placeholder={t("limite da aplicação")}
                value={process.limits.memory}
                onValue={(memory) => updateLimits(index(), { memory })}
              />
              <div class="row-between">
                <span />
                <IconButton
                  icon="trash"
                  label={t("Remover processo")}
                  disabled={list().length === 1}
                  onClick={() => props.onChange(list().filter((_, position) => position !== index()))}
                />
              </div>
            </div>
          </div>
        )}
      </For>
      <span class="field-error">{props.error}</span>
    </Section>
  );
}
