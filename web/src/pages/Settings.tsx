import { GitProviders } from "./settings/GitProviders";
import { GitConnections } from "./settings/GitConnections";
import { EngineSettings } from "./settings/EngineSettings";
import { createSignal } from "solid-js";
import { S3Settings } from "./settings/S3Settings";
import { Button } from "../components/Button";
import { QueryView } from "../components/Feedback";
import { PageHeader, Section } from "../components/Page";
import { api } from "../lib/api";
import { createAction, createQuery } from "../lib/query";
import { currentUser, signIn } from "../lib/state";
import type { InstanceSettings, User } from "../lib/types";
import { language, LANGUAGES, setLanguage, t, type Language } from "../i18n";
import { Select, TextInput } from "../components/Form";

export function SettingsPage() {
  const settings = createQuery(
    () => true,
    () => api.get<InstanceSettings>("/settings"),
  );
  return (
    <div class="page">
      <PageHeader title={t("Configurações")} description={t("Preferências da instância e da sua conta.")} />
      <QueryView query={settings}>{(data) => <InstanceForm settings={data()} />}</QueryView>
      <InstanceBackup />
      <S3Settings />
      <GitProviders />
      <GitConnections />
      <EngineSettings />
      <InterfaceLanguage />
      <ProfileForm />
      <PasswordForm />
    </div>
  );
}

function InstanceForm(props: { settings: InstanceSettings }) {
  const [name, setName] = createSignal(props.settings.instance_name);
  const [publicUrl, setPublicUrl] = createSignal(props.settings.public_url);
  const [serverLanguage, setServerLanguage] = createSignal(props.settings.language);
  const save = createAction(
    () =>
      api.patch<InstanceSettings>("/settings", {
        instance_name: name(),
        public_url: publicUrl(),
        language: serverLanguage(),
      }),
    { success: t("Configurações salvas.") },
  );
  return (
    <Section title={t("Instância")}>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void save.run();
        }}
      >
        <TextInput label={t("Nome da instância")} required value={name()} onValue={setName} error={save.errors().instance_name} />
        <TextInput
          label={t("URL pública do Pier")}
          mono
          placeholder={t("https://pier.exemplo.com")}
          hint={t("Obrigatória para webhooks gerenciados de GitLab, Gitea e Bitbucket. Informe o endereço pelo qual os provedores chegam ao Pier.")}
          value={publicUrl()}
          onValue={setPublicUrl}
          error={save.errors().public_url}
        />
        <Select
          label={t("Idioma do servidor")}
          hint={t("Usado nos logs de deploy, nas notificações e nas respostas da API a quem não informa um idioma.")}
          value={serverLanguage()}
          onValue={(value) => setServerLanguage(value as Language)}
          options={LANGUAGES.map((entry) => ({ value: entry.id, label: entry.label }))}
          error={save.errors().language}
        />
        <div class="form-actions">
          <Button type="submit" variant="primary" pending={save.pending()}>{t("Salvar")}
          </Button>
        </div>
      </form>
    </Section>
  );
}

/** The language of this browser's interface; each person picks their own. */
function InterfaceLanguage() {
  return (
    <Section title={t("Idioma da interface")}>
      <div style={{ "max-width": "360px" }}>
        <Select
          label={t("Idioma")}
          hint={t("Vale para este navegador. A página recarrega ao trocar.")}
          value={language}
          onValue={(value) => setLanguage(value as Language)}
          options={LANGUAGES.map((entry) => ({ value: entry.id, label: entry.label }))}
        />
      </div>
    </Section>
  );
}

function ProfileForm() {
  const [name, setName] = createSignal(currentUser()?.name ?? "");
  const [email, setEmail] = createSignal(currentUser()?.email ?? "");
  const save = createAction(() => api.patch<User>("/me", { name: name(), email: email() }), {
    success: t("Perfil atualizado."),
    onSuccess: signIn,
  });
  return (
    <Section title={t("Seu perfil")}>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void save.run();
        }}
      >
        <div class="grid-2">
          <TextInput label={t("Nome")} required value={name()} onValue={setName} error={save.errors().name} />
          <TextInput label="E-mail" type="email" required value={email()} onValue={setEmail} error={save.errors().email} />
        </div>
        <div class="form-actions">
          <Button type="submit" pending={save.pending()}>{t("Salvar perfil")}
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
    success: t("Senha alterada. As outras sessões foram encerradas."),
    onSuccess: () => {
      setCurrent("");
      setNext("");
    },
  });
  return (
    <Section title={t("Senha")}>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void save.run();
        }}
      >
        <div class="grid-2">
          <TextInput
            label={t("Senha atual")}
            type="password"
            autocomplete="current-password"
            required
            value={current()}
            onValue={setCurrent}
          />
          <TextInput
            label={t("Nova senha")}
            type="password"
            autocomplete="new-password"
            required
            hint={t("Mínimo de 10 caracteres.")}
            value={next()}
            onValue={setNext}
            error={save.errors().password}
          />
        </div>
        <div class="form-actions">
          <Button type="submit" pending={save.pending()}>{t("Alterar senha")}
          </Button>
        </div>
      </form>
    </Section>
  );
}

interface BackupConfig {directory:string;cron:string;retention:number;has_passphrase:boolean}
function InstanceBackup() {
 const config=createQuery(()=>true,()=>api.get<BackupConfig>("/instance/backups/config"));
 const [passphrase,setPassphrase]=createSignal("");
 const download=createAction(async()=>{
 const response=await fetch("/api/v1/instance/backups",{method:"POST",credentials:"same-origin",headers:{"Content-Type":"application/json","X-Pier-Client":"web","Accept-Language":language},body:JSON.stringify({passphrase:passphrase()})});
 if(!response.ok){const payload=await response.json().catch(()=>null);throw new Error(payload?.error?.message ?? t("Não foi possível criar o backup da instância."));}
 const url=URL.createObjectURL(await response.blob());const link=document.createElement("a");link.href=url;link.download="pier-instance.pierbak";document.body.append(link);link.click();link.remove();window.setTimeout(()=>URL.revokeObjectURL(url),1000);setPassphrase("");
 },{success:t("Backup da instância criado.")});
 return <section id="instance-backup" class="stack"><Section title={t("Backup da instância")} description={t("Inclui o banco do Pier e a chave das credenciais. Guarde a frase-senha separadamente. Os volumes das aplicações não estão incluídos.")}>
 <form class="stack" onSubmit={(e)=>{e.preventDefault();void download.run();}}>
 <TextInput label={t("Frase-senha do backup")} type="password" required autocomplete="new-password" value={passphrase()} onValue={setPassphrase} hint={t("Use pelo menos 16 caracteres. A restauração é feita com o Pier parado.")} />
 <div class="form-actions"><Button type="submit" pending={download.pending()}>{t("Baixar backup criptografado")}</Button></div>
 </form>
 <QueryView query={config}>{(data)=><AutomaticBackup config={data()} onSaved={()=>void config.refetch()} />}</QueryView>
 </Section></section>;
}
function AutomaticBackup(props:{config:BackupConfig;onSaved:()=>void}) {
 const [directory,setDirectory]=createSignal(props.config.directory);
 const [cron,setCron]=createSignal(props.config.cron);
 const [retention,setRetention]=createSignal(String(props.config.retention));
 const [passphrase,setPassphrase]=createSignal("");
 const save=createAction(()=>api.patch("/instance/backups/config",{directory:directory(),cron:cron(),retention:Number(retention()),passphrase:passphrase()}),{success:t("Agenda de backup salva."),onSuccess:()=>{setPassphrase("");props.onSaved();}});
 return <form class="stack" onSubmit={(e)=>{e.preventDefault();void save.run();}}>
 <h3>{t("Backup automático local")}</h3>
 <TextInput label={t("Pasta de destino")} value={directory()} onValue={setDirectory} hint={t("Caminho absoluto no servidor do Pier. Vazio desativa a agenda. Copie os arquivos para outro local regularmente.")} />
 <TextInput label={t("Agenda cron")} value={cron()} onValue={setCron} />
 <TextInput label={t("Backups mantidos")} type="number" value={retention()} onValue={setRetention} />
 <TextInput label={t("Frase-senha do backup automático")} type="password" autocomplete="new-password" value={passphrase()} onValue={setPassphrase} hint={props.config.has_passphrase?t("Deixe vazio para manter a frase-senha atual."):t("Use pelo menos 16 caracteres.")} />
 <div class="form-actions"><Button type="submit" pending={save.pending()}>{t("Salvar agenda")}</Button></div>
 </form>;
}
