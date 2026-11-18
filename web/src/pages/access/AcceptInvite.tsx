import { createSignal } from "solid-js";
import { api } from "../../lib/api";
import { createAction } from "../../lib/query";
import { t } from "../../i18n";
import { Button } from "../../components/Button";
import { TextInput } from "../../components/Form";
import { Section } from "../../components/Page";
import { refreshAccess } from "./permissions";

/** Redeems a project invitation for the signed-in account. */
export function AcceptInvite() {
  const [token, setToken] = createSignal("");
  const accept = createAction(() => api.post("/access/accept", { token: token() }), {
    success: t("Convite aceito. O projeto já aparece em Projetos."),
    onSuccess: () => {
      setToken("");
      void refreshAccess();
    },
  });
  return (
    <Section title={t("Aceitar convite")} description={t("Cole o token que a administração do projeto enviou para você.")}>
      <form class="row" onSubmit={(event) => { event.preventDefault(); void accept.run(); }}>
        <div class="grow">
          <TextInput label={t("Token do convite")} type="password" required mono value={token()} onValue={setToken} />
        </div>
        <Button type="submit" pending={accept.pending()} class="align-end">{t("Aceitar convite")}</Button>
      </form>
    </Section>
  );
}
