import { createEffect, createSignal, For, Show } from "solid-js";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import { t } from "../../i18n";
import { Button, IconButton } from "../../components/Button";
import { Alert, QueryView } from "../../components/Feedback";
import { Select, TextArea, TextInput } from "../../components/Form";
import { Modal } from "../../components/Modal";
import { CopyField, Section } from "../../components/Page";
import type { InstanceMember } from "./InstanceRoles";
import { formatRoleMappings, parseRoleMappings } from "./roleMapping";

export interface OIDCSettings {
  configured: boolean;
  issuer: string;
  client_id: string;
  client_secret_set: boolean;
  callback: string;
  role_claim: string;
  role_mappings: Record<string, string>;
  default_role: string;
  allowed_issuers: string[];
  expected_callback: string;
  links: { issuer: string; subject: string; user_id: string; email: string }[];
}

/** OpenID Connect sign-in for the whole instance; configured by the owner only. */
export function SSOSettings(props: { members: InstanceMember[] }) {
  const settings = createQuery(() => true, () => api.get<OIDCSettings>("/access/oidc"));
  return (
    <Section
      title={t("SSO OpenID Connect")}
      description={t("Somente o proprietário da instância configura SSO. Autorize cada identidade pelo subject; emails não criam vínculos.")}
    >
      <QueryView query={settings}>
        {(data) => (
          <Show
            when={data().allowed_issuers.length > 0 && data().expected_callback}
            fallback={<Alert tone="info">{t("SSO está desligado neste servidor. Defina PIER_OIDC_ISSUERS e PIER_OIDC_CALLBACK no ambiente do Pier e reinicie o painel.")}</Alert>}
          >
            <SSOForm settings={data()} members={props.members} onChanged={() => void settings.refetch()} />
          </Show>
        )}
      </QueryView>
    </Section>
  );
}

function SSOForm(props: { settings: OIDCSettings; members: InstanceMember[]; onChanged: () => void }) {
  const [issuer, setIssuer] = createSignal("");
  const [client, setClient] = createSignal("");
  const [secret, setSecret] = createSignal("");
  const [roleClaim, setRoleClaim] = createSignal("groups");
  const [roleLines, setRoleLines] = createSignal("");
  const [defaultRole, setDefaultRole] = createSignal("");
  const [subject, setSubject] = createSignal("");
  const [user, setUser] = createSignal("");
  const [confirmDisable, setConfirmDisable] = createSignal(false);
  createEffect(() => {
    const current = props.settings;
    setIssuer(current.issuer || current.allowed_issuers[0] || "");
    setClient(current.client_id);
    setRoleClaim(current.role_claim);
    setRoleLines(formatRoleMappings(current.role_mappings));
    setDefaultRole(current.default_role);
  });
  // A stored secret is kept only while issuer and client stay the same (the API enforces it).
  const keepsSecret = () =>
    props.settings.client_secret_set && issuer() === props.settings.issuer && client() === props.settings.client_id;

  const save = createAction(async () => {
    const parsed = parseRoleMappings(roleLines());
    if (parsed.invalid.length > 0) {
      throw new Error(t("Linhas de mapeamento inválidas: {lines}", { lines: parsed.invalid.join(", ") }));
    }
    await api.put("/access/oidc", {
      issuer: issuer(), client_id: client(), client_secret: secret(), callback: props.settings.expected_callback,
      role_claim: roleClaim(), role_mappings: parsed.mappings, default_role: defaultRole(),
    });
    setSecret("");
  }, { success: t("Alteração salva."), onSuccess: props.onChanged });
  const link = createAction(() => api.post("/access/oidc/links", { issuer: issuer(), subject: subject(), user_id: user() }), {
    success: t("Identidade autorizada."),
    onSuccess: () => {
      setSubject("");
      props.onChanged();
    },
  });
  const unlink = createAction((target: { issuer: string; subject: string }) => api.post("/access/oidc/unlink", target), {
    success: t("Identidade revogada."),
    onSuccess: props.onChanged,
  });
  const disable = createAction(() => api.delete("/access/oidc"), {
    success: t("SSO desativado."),
    onSuccess: () => {
      setConfirmDisable(false);
      props.onChanged();
    },
  });
  const roleOptions = [
    { value: "viewer", label: t("Somente leitura") },
    { value: "developer", label: t("Desenvolvedor") },
    { value: "admin", label: t("Administrador") },
  ];

  return (
    <div class="stack">
      <form class="stack" onSubmit={(event) => { event.preventDefault(); void save.run(); }}>
        <div class="grid-2">
          <Select label={t("Issuer permitido")} value={issuer()} onValue={setIssuer}
            options={props.settings.allowed_issuers.map((value) => ({ value, label: value }))}
            hint={t("Lista definida em PIER_OIDC_ISSUERS.")} />
          <CopyField label={t("Callback HTTPS")} value={props.settings.expected_callback}
            hint={t("Cadastre este endereço no provedor.")} />
          <TextInput label={t("Client ID")} required value={client()} onValue={setClient} />
          <TextInput label={t("Client secret")} type="password" autocomplete="new-password" required={!keepsSecret()}
            value={secret()} onValue={setSecret}
            placeholder={keepsSecret() ? "••••••••" : undefined}
            hint={keepsSecret() ? t("Deixe vazio para manter o segredo salvo.") : t("Obrigatório ao configurar ou trocar o issuer ou o Client ID.")} />
        </div>
        <h3 class="section-title">{t("Papéis pelo SSO")}</h3>
        <div class="grid-2">
          <TextInput label={t("Claim dos grupos")} value={roleClaim()} onValue={setRoleClaim} mono />
          <Select label={t("Papel padrão do SSO")} value={defaultRole()} onValue={setDefaultRole} placeholder={t("Nenhum")} options={roleOptions} />
        </div>
        <TextArea label={t("Mapeamento de papéis")} rows={3} mono value={roleLines()} onValue={setRoleLines} placeholder="devops=admin"
          hint={t("Uma linha grupo=papel (admin, developer ou viewer). O SSO nunca concede proprietário. Sem mapeamento, os papéis ficam só no Pier.")} />
        <div class="form-actions">
          <Show when={props.settings.configured}>
            <Button variant="ghost" onClick={() => setConfirmDisable(true)}>{t("Desativar SSO")}</Button>
            <a class="btn btn-ghost" href="/api/v1/access/oidc/start">{t("Testar entrada com SSO")}</a>
          </Show>
          <Button type="submit" variant="primary" pending={save.pending()}>{t("Salvar SSO")}</Button>
        </div>
      </form>

      <hr class="divider" />
      <h3 class="section-title">{t("Identidades autorizadas")}</h3>
      <Show when={props.settings.links.length > 0} fallback={<p class="small muted">{t("Nenhuma identidade autorizada ainda.")}</p>}>
        <div class="list">
          <For each={props.settings.links}>
            {(item) => (
              <div class="list-item">
                <div class="grow stack-xs">
                  <strong class="truncate">{item.email}</strong>
                  <span class="small muted mono truncate">{item.subject} · {item.issuer}</span>
                </div>
                <IconButton icon="trash" label={t("Revogar identidade SSO de {email}", { email: item.email })}
                  onClick={() => void unlink.run({ issuer: item.issuer, subject: item.subject })} />
              </div>
            )}
          </For>
        </div>
      </Show>
      <Show when={props.settings.configured}>
        <form class="stack" onSubmit={(event) => { event.preventDefault(); void link.run(); }}>
          <div class="grid-2">
            <TextInput label={t("Subject autorizado")} required mono value={subject()} onValue={setSubject}
              hint={t("O identificador estável da pessoa no provedor (claim sub).")} />
            <Select label={t("Usuário do Pier")} value={user()} onValue={setUser} placeholder={t("Escolha")}
              options={props.members.map((member) => ({ value: member.user_id, label: member.email }))} />
          </div>
          <div class="form-actions">
            <Button type="submit" variant="primary" pending={link.pending()} disabled={!user()}>{t("Autorizar identidade SSO")}</Button>
          </div>
        </form>
      </Show>

      <Show when={confirmDisable()}>
        <Modal title={t("Desativar SSO")} onClose={() => setConfirmDisable(false)}>
          <div class="stack">
            <p>{t("Ninguém poderá entrar pelo provedor até o SSO ser configurado de novo. Senhas e sessões abertas continuam valendo.")}</p>
            <div class="form-actions">
              <Button onClick={() => setConfirmDisable(false)}>{t("Cancelar")}</Button>
              <Button variant="danger" pending={disable.pending()} onClick={() => void disable.run()}>{t("Desativar SSO")}</Button>
            </div>
          </div>
        </Modal>
      </Show>
    </div>
  );
}
