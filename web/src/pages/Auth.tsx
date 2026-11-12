import { LanguageSwitch } from "../components/LanguageSwitch";
import { createSignal, type JSX, Show } from "solid-js";
import { Button } from "../components/Button";
import { Icon } from "../components/Icon";
import { api } from "../lib/api";
import { createAction, createQuery } from "../lib/query";
import { signIn } from "../lib/state";
import type { User } from "../lib/types";
import { t } from "../i18n";
import { TextInput } from "../components/Form";

function AuthLayout(
  props: { title: string; subtitle: string; children: JSX.Element },
) {
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
        <div class="row" style={{ "justify-content": "center" }}>
          <LanguageSwitch />
        </div>
      </div>
    </main>
  );
}

export function LoginPage() {
  const status = createQuery(
    () => true,
    () => api.get<{ password_reset: boolean }>("/auth/status"),
  );
  const [email, setEmail] = createSignal("");
  const [password, setPassword] = createSignal("");
  const [challenge, setChallenge] = createSignal("");
  const [code, setCode] = createSignal("");
  const [resetToken, setResetToken] = createSignal(
    new URLSearchParams(location.hash.slice(1)).get("reset") ?? "",
  );
  if (resetToken()) {
    history.replaceState(null, "", location.pathname + location.search);
  }
  const [resetMode, setResetMode] = createSignal(false);
  const [sent, setSent] = createSignal(false);
  const login = createAction(async () => {
    if (resetToken()) {
      await api.post("/auth/password-reset/confirm", {
        token: resetToken(),
        password: password(),
      });
      setResetToken("");
      setPassword("");
      return;
    }
    if (resetMode()) {
      await api.post("/auth/password-reset", { email: email() });
      setSent(true);
      return;
    }
    if (challenge()) {
      signIn(
        await api.post<User>("/auth/login/2fa", {
          challenge: challenge(),
          code: code(),
        }),
      );
      return;
    }
    const result = await api.post<
      User | { two_factor_required: true; challenge: string }
    >("/auth/login", { email: email(), password: password() });
    setPassword("");
    if ("two_factor_required" in result) setChallenge(result.challenge);
    else signIn(result);
  });

  return (
    <AuthLayout
      title={t("Entrar no Pier")}
      subtitle={t("Seu painel de deploy.")}
    >
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void login.run();
        }}
      >
        <Show when={!challenge() && !resetToken()}>
          <TextInput
            label="E-mail"
            type="email"
            autocomplete="username"
            required
            value={email()}
            onValue={setEmail}
          />
        </Show>
        <Show when={challenge()}>
          <TextInput
            label={t("Código do autenticador ou de recuperação")}
            autocomplete="one-time-code"
            required
            value={code()}
            onValue={setCode}
            error={login.errors().code}
          />
        </Show>
        <Show when={!challenge() && !resetMode()}>
          <TextInput
            label={t("Senha")}
            type="password"
            autocomplete={resetToken() ? "new-password" : "current-password"}
            required
            value={password()}
            onValue={setPassword}
          />
        </Show>
        <Show when={sent()}>
          <p>
            {t("Se a conta existir, você receberá um link de redefinição.")}
          </p>
        </Show>
        <Button
          type="submit"
          variant="primary"
          pending={login.pending()}
          disabled={sent()}
        >
          {resetToken()
            ? t("Redefinir senha")
            : resetMode()
            ? t("Enviar link")
            : t("Entrar")}
        </Button>
        <Show
          when={challenge() || resetMode() || resetToken() ||
            status.data()?.password_reset}
        >
          <Button
            onClick={() => {
              setResetMode(!resetMode());
              setChallenge("");
              setResetToken("");
              setSent(false);
              setPassword("");
            }}
          >
            {resetMode() || challenge()
              ? t("Voltar ao login")
              : t("Esqueci minha senha")}
          </Button>
        </Show>
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
    () =>
      api.post<User>("/auth/setup", {
        setup_code: code(),
        name: name(),
        email: email(),
        password: password(),
      }),
    { onSuccess: signIn },
  );

  return (
    <AuthLayout
      title={t("Bem-vindo ao Pier")}
      subtitle={t("Crie a conta de administrador para começar.")}
    >
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void setup.run();
        }}
      >
        <TextInput
          label={t("Código de configuração")}
          hint={t(
            "Aparece no log do Pier ao iniciar (no Docker: docker logs pier). Prova que você controla esta instalação.",
          )}
          autocomplete="off"
          placeholder="XXXX-XXXX-XXXX"
          required
          value={code()}
          onValue={setCode}
          error={setup.errors().setup_code}
        />
        <TextInput
          label={t("Nome")}
          autocomplete="name"
          required
          value={name()}
          onValue={setName}
          error={setup.errors().name}
        />
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
          label={t("Senha")}
          type="password"
          autocomplete="new-password"
          required
          hint={t("Mínimo de 10 caracteres.")}
          value={password()}
          onValue={setPassword}
          error={setup.errors().password}
        />
        <Button type="submit" variant="primary" pending={setup.pending()}>
          {t("Criar conta e entrar")}
        </Button>
      </form>
    </AuthLayout>
  );
}
