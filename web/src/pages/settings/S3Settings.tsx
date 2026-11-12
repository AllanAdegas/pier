import { createSignal } from "solid-js";
import { Button } from "../../components/Button";
import { TextInput } from "../../components/Form";
import { QueryView } from "../../components/Feedback";
import { Section } from "../../components/Page";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import { t } from "../../i18n";

type Destination = {endpoint: string; region: string; bucket: string; prefix: string; access_key: string; secret_key: string};
export function S3Settings() {
  const query = createQuery(() => true, () => api.get<Destination>("/settings/backup-s3"));
  return <Section title={t("Destino S3 dos backups")}><QueryView query={query}>{(value) => <S3Form initial={value()} />}</QueryView></Section>;
}
function S3Form(props: {initial: Destination}) {
  const [value, setValue] = createSignal(props.initial);
  const save = createAction(() => api.patch("/settings/backup-s3", value()), {success: t("Destino S3 salvo.")});
  return <form class="stack" onSubmit={(event) => {event.preventDefault(); void save.run();}}>
    <TextInput label={t("Endpoint S3 (HTTPS)")} value={value().endpoint} onValue={(endpoint) => setValue({...value(), endpoint})} />
    <TextInput label={t("Região S3")} value={value().region} onValue={(region) => setValue({...value(), region})} />
    <TextInput label="Bucket" value={value().bucket} onValue={(bucket) => setValue({...value(), bucket})} />
    <TextInput label={t("Prefixo S3")} value={value().prefix} onValue={(prefix) => setValue({...value(), prefix})} />
    <TextInput label={t("Chave de acesso S3")} type="password" value={value().access_key} onValue={(access_key) => setValue({...value(), access_key})} />
    <TextInput label={t("Chave secreta S3")} type="password" hint={t("Deixe as chaves vazias para manter as credenciais salvas.")} value={value().secret_key} onValue={(secret_key) => setValue({...value(), secret_key})} />
    <Button type="submit" pending={save.pending()}>{t("Salvar destino S3")}</Button>
  </form>;
}
