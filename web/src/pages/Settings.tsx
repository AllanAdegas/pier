import { createSignal } from "solid-js";
import { Button } from "../components/Button";
import { QueryView } from "../components/Feedback";
import { TextInput } from "../components/Form";
import { PageHeader, Section } from "../components/Page";
import { api } from "../lib/api";
import { createAction, createQuery } from "../lib/query";
import { currentUser, signIn } from "../lib/state";
import type { InstanceSettings, User } from "../lib/types";

export function SettingsPage() {
  const settings = createQuery(
    () => true,
    () => api.get<InstanceSettings>("/settings"),
  );
  return (
    <div class="page">
      <PageHeader title="Configurações" description="Preferências da instância e da sua conta." />
      <QueryView query={settings}>{(data) => <InstanceForm settings={data()} />}</QueryView>
      <ProfileForm />
      <PasswordForm />
    </div>
  );
}

function InstanceForm(props: { settings: InstanceSettings }) {
  const [name, setName] = createSignal(props.settings.instance_name);
  const [publicUrl, setPublicUrl] = createSignal(props.settings.public_url);
  const save = createAction(
    () => api.patch<InstanceSettings>("/settings", { instance_name: name(), public_url: publicUrl() }),
    { success: "Configurações salvas." },
  );
  return (
    <Section title="Instância">
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void save.run();
        }}
      >
        <TextInput label="Nome da instância" required value={name()} onValue={setName} error={save.errors().instance_name} />
        <TextInput
          label="URL pública do Pier"
          mono
          placeholder="https://pier.exemplo.com"
          hint="Usada para montar as URLs de webhook mostradas nas aplicações. Vazio, usa o endereço pelo qual você acessa o painel."
          value={publicUrl()}
          onValue={setPublicUrl}
          error={save.errors().public_url}
        />
        <div class="form-actions">
          <Button type="submit" variant="primary" pending={save.pending()}>
            Salvar
          </Button>
        </div>
      </form>
    </Section>
  );
}

function ProfileForm() {
  const [name, setName] = createSignal(currentUser()?.name ?? "");
  const [email, setEmail] = createSignal(currentUser()?.email ?? "");
  const save = createAction(() => api.patch<User>("/me", { name: name(), email: email() }), {
    success: "Perfil atualizado.",
    onSuccess: signIn,
  });
  return (
    <Section title="Seu perfil">
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void save.run();
        }}
      >
        <div class="grid-2">
          <TextInput label="Nome" required value={name()} onValue={setName} error={save.errors().name} />
          <TextInput label="E-mail" type="email" required value={email()} onValue={setEmail} error={save.errors().email} />
        </div>
        <div class="form-actions">
          <Button type="submit" pending={save.pending()}>
            Salvar perfil
          </Button>
        </div>
      </form>
    </Section>
  );
}

function PasswordForm() {
  const [current, setCurrent] = createSignal("");
  const [next, setNext] = createSignal("");
  const save = createAction(() => api.post("/me/password", { current: current(), next: next() }), {
    success: "Senha alterada. As outras sessões foram encerradas.",
    onSuccess: () => {
      setCurrent("");
      setNext("");
    },
  });
  return (
    <Section title="Senha">
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void save.run();
        }}
      >
        <div class="grid-2">
          <TextInput
            label="Senha atual"
            type="password"
            autocomplete="current-password"
            required
            value={current()}
            onValue={setCurrent}
          />
          <TextInput
            label="Nova senha"
            type="password"
            autocomplete="new-password"
            required
            hint="Mínimo de 10 caracteres."
            value={next()}
            onValue={setNext}
            error={save.errors().password}
          />
        </div>
        <div class="form-actions">
          <Button type="submit" pending={save.pending()}>
            Alterar senha
          </Button>
        </div>
      </form>
    </Section>
  );
}
