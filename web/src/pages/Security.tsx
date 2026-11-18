import { createSignal, For, Match, Show, Switch } from "solid-js";
import { useSearchParams } from "@solidjs/router";
import { Button, IconButton } from "../components/Button";
import { Alert, EmptyState, QueryView } from "../components/Feedback";
import { ConfirmDelete, Modal } from "../components/Modal";
import { CopyField, PageHeader, Section, Tabs } from "../components/Page";
import { api } from "../lib/api";
import { formatDateTime, formatRelative } from "../lib/format";
import { createAction, createQuery } from "../lib/query";
import { currentUser } from "../lib/state";
import type { ApiToken, PrivateKey, User } from "../lib/types";
import { AccountSecurity } from "./security/AccountSecurity";
import { AuditSection } from "./security/AuditSection";
import { McpSection } from "./security/McpSection";
import { TokenForm } from "./security/TokenForm";
import { t } from "../i18n";
import { Checkbox, TextArea, TextInput } from "../components/Form";
import { AcceptInvite } from "./access/AcceptInvite";
import { createInstanceRoles, InstanceRoleSelect } from "./access/InstanceRoles";
import { allowed } from "./access/permissions";
import { SSOSettings } from "./access/SSO";

export function SecurityPage() {
  const [tokensVersion, setTokensVersion] = createSignal(0);
  const [search, setSearch] = useSearchParams<{ tab?: string }>();
  const tab = () => search.tab ?? "account";
  const tabs = () => [
    { id: "account", label: t("Sua conta") },
    { id: "access", label: t("Chaves e tokens") },
    { id: "users", label: t("Usuários") },
    { id: "audit", label: t("Auditoria") },
  ];
  return (
    <div class="page">
      <PageHeader title={t("Segurança")} description={t("Chaves SSH, tokens de API, acesso de IA e quem pode entrar no painel.")} />
      <Tabs tabs={tabs()} selected={tab()} onSelect={(id) => setSearch({ tab: id })} />
      <Switch>
        <Match when={tab() === "account"}>
          <AccountSecurity />
          <AcceptInvite />
        </Match>
        <Match when={tab() === "access"}>
          <KeysSection />
          <TokensSection version={tokensVersion()} />
          <McpSection onTokenCreated={() => setTokensVersion(tokensVersion() + 1)} />
        </Match>
        <Match when={tab() === "users"}>
          <UsersSection />
        </Match>
        <Match when={tab() === "audit"}>
          <AuditSection />
        </Match>
      </Switch>
    </div>
  );
}

function KeysSection() {
  const keys = createQuery(
    () => true,
    () => api.get<PrivateKey[]>("/keys"),
  );
  const [adding, setAdding] = createSignal(false);
  const [shown, setShown] = createSignal<PrivateKey | null>(null);
  const remove = createAction((id: string) => api.delete(`/keys/${id}`), {
    success: t("Chave removida."),
    onSuccess: () => void keys.refetch(),
  });

  return (
    <Section
      title={t("Chaves SSH")}
      description={t("Usadas para acessar servidores e clonar repositórios privados. A chave privada nunca sai do Pier.")}
      actions={
        <Button small icon="plus" onClick={() => setAdding(true)}>{t("Nova chave")}
        </Button>
      }
    >
      <QueryView query={keys}>
        {(list) => (
          <Show
            when={list().length > 0}
            fallback={
              <EmptyState icon="key" title={t("Nenhuma chave")}>{t("Gere uma chave e cadastre a parte pública no servidor ou como deploy key do repositório.")}
              </EmptyState>
            }
          >
            <div class="list">
              <For each={list()}>
                {(key) => (
                  <div class="list-item">
                    <div class="grow stack-xs">
                      <strong class="truncate">{key.name}</strong>
                      <span class="small muted mono truncate">{key.fingerprint}</span>
                    </div>
                    <Button small onClick={() => setShown(key)}>{t("Ver chave pública")}
                    </Button>
                    <IconButton icon="trash" label={t("Remover {name}", { name: key.name })} onClick={() => void remove.run(key.id)} />
                  </div>
                )}
              </For>
            </div>
          </Show>
        )}
      </QueryView>
      <Show when={adding()}>
        <KeyForm
          onClose={() => setAdding(false)}
          onSaved={(key) => {
            setAdding(false);
            setShown(key);
            void keys.refetch();
          }}
        />
      </Show>
      <Show when={shown()}>
        {(key) => (
          <Modal title={t("Chave pública — {name}", { name: key().name })} onClose={() => setShown(null)} wide>
            <div class="stack">
              <CopyField
                label={t("Chave pública")}
                value={key().public_key}
                hint={t("Para servidores: adicione em ~/.ssh/authorized_keys. Para repositórios: cadastre como deploy key (somente leitura).")}
              />
              <div class="form-actions">
                <Button variant="primary" onClick={() => setShown(null)}>{t("Concluir")}
                </Button>
              </div>
            </div>
          </Modal>
        )}
      </Show>
    </Section>
  );
}

function KeyForm(props: { onClose: () => void; onSaved: (key: PrivateKey) => void }) {
  const [name, setName] = createSignal("");
  const [importing, setImporting] = createSignal(false);
  const [privateKey, setPrivateKey] = createSignal("");
  const save = createAction(
    () => api.post<PrivateKey>("/keys", { name: name(), private_key: importing() ? privateKey() : "" }),
    { success: t("Chave salva."), onSuccess: props.onSaved },
  );
  return (
    <Modal title={t("Nova chave SSH")} onClose={props.onClose}>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void save.run();
        }}
      >
        <TextInput label={t("Nome")} required placeholder={t("deploy-producao")} value={name()} onValue={setName} error={save.errors().name} />
        <Checkbox
          label={t("Importar uma chave existente")}
          hint={t("Desmarcado, o Pier gera um par ed25519 novo.")}
          checked={importing()}
          onChange={setImporting}
        />
        <Show when={importing()}>
          <TextArea
            label={t("Chave privada")}
            mono
            rows={8}
            placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
            hint={t("Chaves protegidas por senha não são suportadas.")}
            value={privateKey()}
            onValue={setPrivateKey}
            error={save.errors().private_key}
          />
        </Show>
        <div class="form-actions">
          <Button onClick={props.onClose}>{t("Cancelar")}</Button>
          <Button type="submit" variant="primary" pending={save.pending()}>
            {importing() ? t("Importar chave") : t("Gerar chave")}
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function TokensSection(props: { version: number }) {
  const tokens = createQuery(
    () => props.version + 1,
    () => api.get<ApiToken[]>("/tokens"),
  );
  const [adding, setAdding] = createSignal(false);
  const [created, setCreated] = createSignal<ApiToken | null>(null);
  const remove = createAction((id: string) => api.delete(`/tokens/${id}`), {
    success: t("Token revogado."),
    onSuccess: () => void tokens.refetch(),
  });

  return (
    <Section
      title={t("Tokens de API")}
      description={t("Para automações e CI. Envie no cabeçalho Authorization: Bearer <token>.")}
      actions={
        <Button small icon="plus" onClick={() => setAdding(true)}>{t("Novo token")}
        </Button>
      }
    >
      <QueryView query={tokens}>
        {(list) => (
          <Show when={list().length > 0} fallback={<p class="muted">{t("Nenhum token criado.")}</p>}>
            <div class="list">
              <For each={list()}>
                {(token) => (
                  <div class="list-item">
                    <div class="grow stack-xs">
                      <strong class="truncate">{token.name}</strong>
                      <span class="small muted">
                        {token.last_used_at ? t("Usado {name}", { name: formatRelative(token.last_used_at) }) : t("Nunca usado")} ·{" "}
                        {t("criado em {date}", { date: formatDateTime(token.created_at) })}
                      </span>
                    </div>
                    <For each={token.abilities}>{(ability) => <span class="tag">{ability}</span>}</For>
                    <IconButton icon="trash" label={t("Revogar {name}", { name: token.name })} onClick={() => void remove.run(token.id)} />
                  </div>
                )}
              </For>
            </div>
          </Show>
        )}
      </QueryView>
      <Show when={adding()}>
        <TokenForm
          onClose={() => setAdding(false)}
          onSaved={(token) => {
            setAdding(false);
            setCreated(token);
            void tokens.refetch();
          }}
        />
      </Show>
      <Show when={created()}>
        {(token) => (
          <Modal title={t("Token criado")} onClose={() => setCreated(null)} wide>
            <div class="stack">
              <Alert tone="warning">{t("Copie o token agora. Por segurança, ele não será exibido novamente.")}</Alert>
              <CopyField label={token().name} value={token().secret ?? ""} />
              <div class="form-actions">
                <Button variant="primary" onClick={() => setCreated(null)}>{t("Já copiei")}
                </Button>
              </div>
            </div>
          </Modal>
        )}
      </Show>
    </Section>
  );
}

function UsersSection() {
  const users = createQuery(
    () => true,
    () => api.get<User[]>("/users"),
  );
  const isOwner = () => allowed("write", undefined, "owner");
  const roles = createInstanceRoles(isOwner);
  const [adding, setAdding] = createSignal(false);
  const [removing, setRemoving] = createSignal<User | null>(null);
  const remove = createAction((id: string) => api.delete(`/users/${id}`), {
    success: t("Usuário removido."),
    onSuccess: () => {
      setRemoving(null);
      void users.refetch();
      void roles.members.refetch();
    },
  });

  return (
    <>
      <Section
        title={t("Usuários")}
        description={t("O papel na instância vale para todos os projetos e para servidores, configurações e usuários. Com “Somente projetos”, a pessoa vê apenas os projetos em que for membro.")}
        actions={
          <Button small icon="plus" onClick={() => setAdding(true)}>{t("Novo usuário")}
          </Button>
        }
      >
        <QueryView query={users}>
          {(list) => (
            <div class="list">
              <For each={list()}>
                {(user) => (
                  <div class="list-item">
                    <div class="grow stack-xs">
                      <strong class="truncate">{user.name}</strong>
                      <span class="small muted truncate">{user.email}</span>
                    </div>
                    <Show when={isOwner() && roles.members.data()}>
                      <InstanceRoleSelect
                        userId={user.id}
                        role={roles.roleOf(user.id) ?? ""}
                        pending={roles.change.pending()}
                        onChange={(role) => void roles.change.run(user.id, role)}
                      />
                    </Show>
                    <Show
                      when={user.id !== currentUser()?.id}
                      fallback={<span class="tag">{t("você")}</span>}
                    >
                      <IconButton icon="trash" label={t("Remover {name}", { name: user.name })} onClick={() => setRemoving(user)} />
                    </Show>
                  </div>
                )}
              </For>
            </div>
          )}
        </QueryView>
        <Show when={adding()}>
          <UserForm
            onClose={() => setAdding(false)}
            onSaved={() => {
              setAdding(false);
              void users.refetch();
              void roles.members.refetch();
            }}
          />
        </Show>
        <Show when={removing()}>
          {(user) => (
            <ConfirmDelete
              title={t("Remover usuário")}
              resourceName={user().email}
              description={t("A pessoa perde o acesso ao painel e as sessões abertas dela são encerradas.")}
              pending={remove.pending()}
              onConfirm={() => void remove.run(user().id)}
              onClose={() => setRemoving(null)}
            />
          )}
        </Show>
      </Section>
      <Show when={isOwner()}>
        <SSOSettings members={roles.members.data() ?? []} />
      </Show>
    </>
  );
}

function UserForm(props: { onClose: () => void; onSaved: () => void }) {
  const [name, setName] = createSignal("");
  const [email, setEmail] = createSignal("");
  const [password, setPassword] = createSignal("");
  const save = createAction(() => api.post<User>("/users", { name: name(), email: email(), password: password() }), {
    success: t("Usuário criado."),
    onSuccess: props.onSaved,
  });
  return (
    <Modal title={t("Novo usuário")} onClose={props.onClose}>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void save.run();
        }}
      >
        <TextInput label={t("Nome")} required value={name()} onValue={setName} error={save.errors().name} />
        <TextInput label="E-mail" type="email" required value={email()} onValue={setEmail} error={save.errors().email} />
        <TextInput
          label={t("Senha inicial")}
          type="password"
          required
          autocomplete="new-password"
          hint={t("Mínimo de 10 caracteres. Peça para a pessoa trocar no primeiro acesso.")}
          value={password()}
          onValue={setPassword}
          error={save.errors().password}
        />
        <div class="form-actions">
          <Button onClick={props.onClose}>{t("Cancelar")}</Button>
          <Button type="submit" variant="primary" pending={save.pending()}>{t("Criar usuário")}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
