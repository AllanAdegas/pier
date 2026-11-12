import { createSignal, For, Show } from "solid-js";
import { Button } from "../../components/Button";
import { TextInput } from "../../components/Form";
import { CopyField, Section } from "../../components/Page";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import { t } from "../../i18n";

interface Session {
  id: string;
  created_at: string;
  last_seen_at: string;
  ip: string;
  user_agent: string;
  current: boolean;
}
interface Enrollment {
  secret: string;
  uri: string;
  qr_svg: string;
}
export function AccountSecurity() {
  const status = createQuery(
    () => true,
    () => api.get<{ enabled: boolean; recovery_codes_left: number }>("/me/2fa"),
  );
  const sessions = createQuery(
    () => true,
    () => api.get<Session[]>("/me/sessions"),
  );
  const [enrollment, setEnrollment] = createSignal<Enrollment | null>(null);
  const [codes, setCodes] = createSignal<string[]>([]);
  const [code, setCode] = createSignal("");
  const [password, setPassword] = createSignal("");
  const refresh = () => {
    setCode("");
    setPassword("");
    void status.refetch();
    void sessions.refetch();
  };
  const enroll = createAction(() => api.post<Enrollment>("/me/2fa/enroll"), {
    onSuccess: setEnrollment,
  });
  const confirm = createAction(
    () =>
      api.post<{ recovery_codes: string[] }>("/me/2fa/confirm", {
        code: code(),
      }),
    {
      onSuccess: (r) => {
        setCodes(r.recovery_codes);
        setEnrollment(null);
        refresh();
      },
    },
  );
  const disable = createAction(
    () => api.post("/me/2fa/disable", { password: password(), code: code() }),
    {
      onSuccess: () => {
        setCodes([]);
        refresh();
      },
    },
  );
  const regenerate = createAction(
    () =>
      api.post<{ recovery_codes: string[] }>("/me/2fa/recovery-codes", {
        code: code(),
      }),
    {
      onSuccess: (r) => {
        setCodes(r.recovery_codes);
        refresh();
      },
    },
  );
  const revoke = createAction(
    (id: string) => api.delete(`/me/sessions/${id}`),
    { onSuccess: () => void sessions.refetch() },
  );
  const others = createAction(() => api.post("/me/sessions/revoke-others"), {
    onSuccess: () => void sessions.refetch(),
  });
  return (
    <>
      <Section
        title={t("Verificação em duas etapas")}
        description={t(
          "Proteja sua conta com um autenticador e guarde os códigos de recuperação.",
        )}
      >
        <div class="stack">
          <Show when={status.data()}>
            {(st) => (
              <>
                <p>
                  {st().enabled
                    ? t("Verificação em duas etapas ativa.")
                    : t("Verificação em duas etapas desativada.")}
                </p>
                <Show when={!st().enabled}>
                  <Button
                    onClick={() => void enroll.run()}
                    pending={enroll.pending()}
                  >
                    {t("Configurar autenticador")}
                  </Button>
                </Show>
                <Show when={st().enabled}>
                  <p>
                    {t("Códigos restantes: {count}", {
                      count: st().recovery_codes_left,
                    })}
                  </p>
                </Show>
              </>
            )}
          </Show>
          <Show when={enrollment()}>
            {(e) => (
              <>
                <img
                  width="220"
                  height="220"
                  alt={t("QR do autenticador")}
                  src={`data:image/svg+xml;charset=utf-8,${
                    encodeURIComponent(e().qr_svg)
                  }`}
                />
                <CopyField
                  label={t("Chave do autenticador")}
                  value={e().secret}
                />
                <p>
                  {t(
                    "Adicione a chave ao autenticador e confirme com o primeiro código.",
                  )}
                </p>
              </>
            )}
          </Show>
          <Show when={enrollment() || status.data()?.enabled}>
            <TextInput
              label={t("Código do autenticador ou de recuperação")}
              autocomplete="one-time-code"
              value={code()}
              onValue={setCode}
            />
            <Show when={enrollment()}>
              <Button
                onClick={() => void confirm.run()}
                pending={confirm.pending()}
              >
                {t("Confirmar ativação")}
              </Button>
            </Show>
            <Show when={status.data()?.enabled}>
              <Button
                onClick={() => void regenerate.run()}
                pending={regenerate.pending()}
              >
                {t("Gerar novos códigos de recuperação")}
              </Button>
              <TextInput
                label={t("Senha")}
                type="password"
                autocomplete="current-password"
                value={password()}
                onValue={setPassword}
              />
              <Button
                variant="danger"
                onClick={() => void disable.run()}
                pending={disable.pending()}
              >
                {t("Desativar verificação em duas etapas")}
              </Button>
            </Show>
          </Show>
          <Show when={codes().length}>
            <p>
              {t(
                "Guarde estes códigos em local seguro. Cada um só funciona uma vez.",
              )}
            </p>
            <pre>{codes().join("\n")}</pre>
            <Button onClick={() => setCodes([])}>
              {t("Já guardei os códigos")}
            </Button>
          </Show>
        </div>
      </Section>
      <Section
        title={t("Sessões da conta")}
        description={t(
          "Encerre sessões de dispositivos que você não reconhece.",
        )}
      >
        <div class="stack">
          <For each={sessions.data() ?? []}>
            {(s) => (
              <div class="list-item">
                <div class="grow">
                  <strong>{s.current ? t("Esta sessão") : s.ip}</strong>
                  <p class="small muted">{s.user_agent} · {s.last_seen_at}</p>
                </div>
                <Show when={!s.current}>
                  <Button small onClick={() => void revoke.run(s.id)}>
                    {t("Encerrar sessão")}
                  </Button>
                </Show>
              </div>
            )}
          </For>
          <Button onClick={() => void others.run()} pending={others.pending()}>
            {t("Encerrar outras sessões")}
          </Button>
        </div>
      </Section>
    </>
  );
}
