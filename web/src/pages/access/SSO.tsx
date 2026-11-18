import { createSignal, Show } from "solid-js";
import { api } from "../../lib/api";
import { createAction } from "../../lib/query";
import { t } from "../../i18n";
import { Button } from "../../components/Button";
import { Select, TextArea, TextInput } from "../../components/Form";
import { Modal } from "../../components/Modal";
import { Section } from "../../components/Page";
import type { InstanceMember } from "./InstanceRoles";
import { parseRoleMappings } from "./roleMapping";

/** OpenID Connect sign-in for the whole instance; configured by the owner only. */
export function SSOSettings(props: { members: InstanceMember[] }) {
  const [issuer, setIssuer] = createSignal("");
  const [client, setClient] = createSignal("");
  const [secret, setSecret] = createSignal("");
  const [callback, setCallback] = createSignal("");
  const [roleClaim, setRoleClaim] = createSignal("groups");
  const [roleLines, setRoleLines] = createSignal("");
  const [defaultRole, setDefaultRole] = createSignal("");
  const [subject, setSubject] = createSignal("");
  const [user, setUser] = createSignal("");
  const [confirmDisable, setConfirmDisable] = createSignal(false);

  const save = createAction(async () => {
    const parsed = parseRoleMappings(roleLines());
    if (parsed.invalid.length > 0) {
      throw new Error(t("Linhas de mapeamento inválidas: {lines}", { lines: parsed.invalid.join(", ") }));
    }
    await api.put("/access/oidc", {
      issuer: issuer(), client_id: client(), client_secret: secret(), callback: callback(),
      role_claim: roleClaim(), role_mappings: parsed.mappings, default_role: defaultRole(),
    });
    setSecret("");
  }, { success: t("Alteração salva.") });
  const link = createAction(() => api.post("/access/oidc/links", { issuer: issuer(), subject: subject(), user_id: user() }), {
    success: t("Identidade autorizada."),
  });
  const unlink = createAction(() => api.post("/access/oidc/unlink", { issuer: issuer(), subject: subject() }), {
    success: t("Identidade revogada."),
  });
  const disable = createAction(() => api.delete("/access/oidc"), {
    success: t("SSO desativado."),
    onSuccess: () => setConfirmDisable(false),
  });
  const roleOptions = [
    { value: "viewer", label: t("Somente leitura") },
    { value: "developer", label: t("Desenvolvedor") },
    { value: "admin", label: t("Administrador") },
  ];

  return (
    <Section
      title={t("SSO OpenID Connect")}
      description={t("Somente o proprietário da instância configura SSO. Autorize cada identidade pelo subject; emails não criam vínculos.")}
      actions={<Button small variant="ghost" onClick={() => setConfirmDisable(true)}>{t("Desativar SSO")}</Button>}
    >
      <form class="stack" onSubmit={(event) => { event.preventDefault(); void save.run(); }}>
        <div class="grid-2">
          <TextInput label={t("Issuer permitido")} type="url" required value={issuer()} onValue={setIssuer} placeholder="https://id.example.com"
            hint={t("Precisa constar em PIER_OIDC_ISSUERS.")} />
          <TextInput label={t("Callback HTTPS")} type="url" required value={callback()} onValue={setCallback}
            placeholder="https://pier.example.com/api/v1/access/oidc/callback"
            hint={t("Igual a PIER_OIDC_CALLBACK.")} />
          <TextInput label={t("Client ID")} required value={client()} onValue={setClient} />
          <TextInput label={t("Client secret")} type="password" autocomplete="new-password" required value={secret()} onValue={setSecret}
            hint={t("Informe a cada gravação: o Pier não mostra o valor salvo.")} />
        </div>
        <h3 class="section-title">{t("Papéis pelo SSO")}</h3>
        <div class="grid-2">
          <TextInput label={t("Claim dos grupos")} value={roleClaim()} onValue={setRoleClaim} mono />
          <Select label={t("Papel padrão do SSO")} value={defaultRole()} onValue={setDefaultRole} placeholder={t("Nenhum")} options={roleOptions} />
        </div>
        <TextArea label={t("Mapeamento de papéis")} rows={3} mono value={roleLines()} onValue={setRoleLines} placeholder="devops=admin"
          hint={t("Uma linha grupo=papel (admin, developer ou viewer). O SSO nunca concede proprietário. Sem mapeamento, os papéis ficam só no Pier.")} />
        <div class="form-actions">
          <Button type="submit" variant="primary" pending={save.pending()}>{t("Salvar SSO")}</Button>
        </div>
      </form>
      <hr class="divider" />
      <form class="stack" onSubmit={(event) => { event.preventDefault(); void link.run(); }}>
        <h3 class="section-title">{t("Identidades autorizadas")}</h3>
        <div class="grid-2">
          <TextInput label={t("Subject autorizado")} required mono value={subject()} onValue={setSubject}
            hint={t("O identificador estável da pessoa no provedor (claim sub).")} />
          <Select label={t("Usuário do Pier")} value={user()} onValue={setUser} placeholder={t("Escolha")}
            options={props.members.map((member) => ({ value: member.user_id, label: member.email }))} />
        </div>
        <div class="form-actions">
          <Button type="submit" variant="primary" pending={link.pending()} disabled={!user()}>{t("Autorizar identidade SSO")}</Button>
          <Button pending={unlink.pending()} disabled={!subject()} onClick={() => void unlink.run()}>{t("Revogar identidade SSO")}</Button>
          <a class="btn btn-ghost" href="/api/v1/access/oidc/start">{t("Testar entrada com SSO")}</a>
        </div>
      </form>
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
    </Section>
  );
}
