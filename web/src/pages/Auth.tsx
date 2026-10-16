import { createSignal, type JSX } from "solid-js";
import { Button } from "../components/Button";
import { TextInput } from "../components/Form";
import { Icon } from "../components/Icon";
import { api } from "../lib/api";
import { createAction } from "../lib/query";
import { signIn } from "../lib/state";
import type { User } from "../lib/types";

function AuthLayout(props: { title: string; subtitle: string; children: JSX.Element }) {
  return (
    <main class="auth">
      <div class="auth-card">
        <div class="auth-brand">
          <span class="brand-mark" style={{ width: "56px", height: "56px" }}>
            <Icon name="anchor" size={30} />
          </span>
          <h1>{props.title}</h1>
          <p class="muted">{props.subtitle}</p>
        </div>
        <div class="card">{props.children}</div>
      </div>
    </main>
  );
}

export function LoginPage() {
  const [email, setEmail] = createSignal("");
  const [password, setPassword] = createSignal("");
  const login = createAction(() => api.post<User>("/auth/login", { email: email(), password: password() }), {
    onSuccess: signIn,
  });

  return (
    <AuthLayout title="Entrar no Pier" subtitle="Seu painel de deploy.">
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void login.run();
        }}
      >
        <TextInput label="E-mail" type="email" autocomplete="username" required value={email()} onValue={setEmail} />
        <TextInput
          label="Senha"
          type="password"
          autocomplete="current-password"
          required
          value={password()}
          onValue={setPassword}
        />
        <Button type="submit" variant="primary" pending={login.pending()}>
          Entrar
        </Button>
      </form>
    </AuthLayout>
  );
}

/** First run: whoever opens the instance first creates the owner account. */
export function SetupPage() {
  const [name, setName] = createSignal("");
  const [email, setEmail] = createSignal("");
  const [password, setPassword] = createSignal("");
  const [code, setCode] = createSignal("");
  const setup = createAction(
    () => api.post<User>("/auth/setup", { setup_code: code(), name: name(), email: email(), password: password() }),
    { onSuccess: signIn },
  );

  return (
    <AuthLayout title="Bem-vindo ao Pier" subtitle="Crie a conta de administrador para começar.">
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void setup.run();
        }}
      >
        <TextInput
          label="Código de configuração"
          hint="Aparece no log do Pier ao iniciar (no Docker: docker logs pier). Prova que você controla esta instalação."
          autocomplete="off"
          placeholder="XXXX-XXXX-XXXX"
          required
          value={code()}
          onValue={setCode}
          error={setup.errors().setup_code}
        />
        <TextInput label="Nome" autocomplete="name" required value={name()} onValue={setName} error={setup.errors().name} />
        <TextInput
          label="E-mail"
          type="email"
          autocomplete="username"
          required
          value={email()}
          onValue={setEmail}
          error={setup.errors().email}
        />
        <TextInput
          label="Senha"
          type="password"
          autocomplete="new-password"
          required
          hint="Mínimo de 10 caracteres."
          value={password()}
          onValue={setPassword}
          error={setup.errors().password}
        />
        <Button type="submit" variant="primary" pending={setup.pending()}>
          Criar conta e entrar
        </Button>
      </form>
    </AuthLayout>
  );
}
