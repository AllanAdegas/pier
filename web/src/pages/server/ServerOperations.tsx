import { createSignal, Show } from "solid-js";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import { QueryView } from "../../components/Feedback";
import { Section } from "../../components/Page";
import { Button } from "../../components/Button";
import { TextInput, NumberInput, Checkbox } from "../../components/Form";
import { t } from "../../i18n";
type Policy = { maintenance: boolean; cleanup_enabled: boolean; cleanup_cron: string; min_age_hours: number };
export function ServerOperations(props: { id: string; name: string }) {
 const base = () => `/servers/${props.id}`;
 const query = createQuery(() => props.id, () => api.get<Policy>(`${base()}/operations-policy`));
 const [report,setReport] = createSignal("");
 const [confirmation,setConfirmation] = createSignal("");
 const save = createAction((policy:Policy) => api.put<Policy>(`${base()}/operations-policy`,policy), {onSuccess:(p) => {query.set(p);}});
 const cleanup = createAction((dry:boolean) => api.post(`${base()}/safe-cleanup`,{dry_run:dry,confirmation:confirmation()}), {onSuccess:(p) => {setReport(JSON.stringify(p,null,2));}});
 return <Section title={t("Operações do servidor")}>
  <QueryView query={query}>{(policy) => <div class="stack">
   <Checkbox label={t("Pausar novos deploys neste servidor")} checked={policy().maintenance} onChange={(v) => query.set({...policy(),maintenance:v})} />
   <p class="muted">{t("A manutenção não interrompe containers em execução nem bloqueia leitura e restauração.")}</p>
   <Checkbox label={t("Agendar limpeza de objetos descartáveis")} checked={policy().cleanup_enabled} onChange={(v) => query.set({...policy(),cleanup_enabled:v})} />
   <TextInput label={t("Agendamento (cron)")} value={policy().cleanup_cron} onValue={(v) => query.set({...policy(),cleanup_cron:v})} />
   <NumberInput label={t("Idade mínima em horas")} value={policy().min_age_hours} min={24} max={8760} onValue={(v) => query.set({...policy(),min_age_hours:v})} />
   <p class="muted">{t("Só entram objetos parados com pier.managed=true, pier.cleanup=true e pier.type=scratch. Imagens precisam estar sem tag. Volumes e backups nunca entram.")}</p>
   <Button pending={save.pending()} onClick={() => void save.run(policy())}>{t("Salvar")}</Button>
   <Button pending={cleanup.pending()} onClick={() => void cleanup.run(true)}>{t("Simular limpeza segura")}</Button>
   <TextInput label={t("Nome do servidor para confirmar")} value={confirmation()} onValue={setConfirmation} />
   <Button disabled={confirmation()!==props.name} pending={cleanup.pending()} onClick={() => void cleanup.run(false)}>{t("Executar limpeza segura")}</Button>
   <Show when={report()}><pre class="log">{report()}</pre></Show>
  </div>}</QueryView>
 </Section>;
}
