import { createSignal, type JSX } from "solid-js";
import { Modal } from "../../components/Modal";
import { TextInput } from "../../components/Form";
import { Button } from "../../components/Button";
import { t } from "../../i18n";

/**
 * Confirmation by typing a name, for actions that cannot be undone or that
 * change a machine (installing Docker, removing objects, destroying a server).
 */
export function ConfirmTyped(props: {
  title: string;
  name: string;
  description: JSX.Element;
  action: string;
  danger?: boolean;
  pending: boolean;
  onConfirm: (typed: string) => void;
  onClose: () => void;
  children?: JSX.Element;
}) {
  const [typed, setTyped] = createSignal("");
  const matches = () => typed().trim() === props.name;
  return (
    <Modal title={props.title} onClose={props.onClose}>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          if (matches()) props.onConfirm(typed().trim());
        }}
      >
        <p>{props.description}</p>
        {props.children}
        <TextInput
          label={t("Digite \"{name}\" para confirmar", { name: props.name })}
          value={typed()}
          onValue={setTyped}
          autocomplete="off"
        />
        <div class="form-actions">
          <Button onClick={props.onClose}>{t("Cancelar")}</Button>
          <Button type="submit" variant={props.danger ? "danger" : "primary"} disabled={!matches()} pending={props.pending}>
            {props.action}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
