import { createResource, createSignal, For, Show } from "solid-js";
import { api, errorMessage } from "../../lib/api";
import { t } from "../../i18n";
import type { Role } from "./can";

type InstanceMember = { user_id: string; email: string; role: Role | "" };

// Instance roles cover every project and the instance itself (servers,
// settings, users). Shown to instance owners only; the API enforces it.
export function InstanceRoles() {
  const [members, { refetch }] = createResource(() => api.get<InstanceMember[]>("/access/instance"));
  const [message, setMessage] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const labels: Record<Role | "", string> = {
    owner: t("Proprietário"), admin: t("Administrador"), developer: t("Desenvolvedor"), viewer: t("Somente leitura"),
    "": t("Somente projetos"),
  };
  async function change(member: InstanceMember, role: string) {
    setBusy(true); setMessage("");
    try { await api.put(`/access/instance/${encodeURIComponent(member.user_id)}`, { role }); await refetch(); }
    catch (error) { setMessage(errorMessage(error)); await refetch(); }
    finally { setBusy(false); }
  }
  return <section><h2>{t("Papéis na instância")}</h2>
    <p>{t("O papel na instância vale para todos os projetos e para servidores, configurações e usuários.")}</p>
    <Show when={message()}><p role="alert">{message()}</p></Show>
    <Show when={members.error}><p role="alert">{errorMessage(members.error)}</p></Show>
    <For each={members()}>{member => <div>
      <span>{member.email}</span>
      <select aria-label={t("Papel na instância")} value={member.role} disabled={busy()} onChange={e => void change(member, e.currentTarget.value)}>
        <For each={Object.keys(labels) as (Role | "")[]}>{r => <option value={r}>{labels[r]}</option>}</For>
      </select>
    </div>}</For>
  </section>;
}
