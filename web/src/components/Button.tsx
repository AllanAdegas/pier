import { Show, splitProps, type JSX } from "solid-js";
import { Icon, type IconName } from "./Icon";

type Variant = "default" | "primary" | "danger" | "ghost";

interface ButtonProps extends JSX.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  small?: boolean;
  icon?: IconName;
  /** Shows a spinner and blocks clicks while an action is running. */
  pending?: boolean;
}

const VARIANT_CLASS: Record<Variant, string> = {
  default: "",
  primary: "btn-primary",
  danger: "btn-danger",
  ghost: "btn-ghost",
};

export function Button(props: ButtonProps) {
  const [own, rest] = splitProps(props, ["variant", "small", "icon", "pending", "children", "class", "disabled"]);
  return (
    <button
      type="button"
      {...rest}
      class={`btn ${VARIANT_CLASS[own.variant ?? "default"]} ${own.small ? "btn-sm" : ""} ${own.class ?? ""}`}
      disabled={own.disabled || own.pending}
      aria-busy={own.pending ? "true" : undefined}
    >
      <Show when={own.pending} fallback={<Show when={own.icon}>{(name) => <Icon name={name()} size={16} />}</Show>}>
        <span class="spinner" aria-hidden="true" />
      </Show>
      {own.children}
    </button>
  );
}

/** Square button that only shows an icon; the label is read by screen readers. */
export function IconButton(props: {
  icon: IconName;
  label: string;
  onClick?: JSX.EventHandler<HTMLButtonElement, MouseEvent>;
  pending?: boolean;
  disabled?: boolean;
  /** For disclosure toggles: whether the content it controls is shown. */
  expanded?: boolean;
  class?: string;
}) {
  return (
    <button
      type="button"
      class={`btn btn-ghost btn-icon ${props.class ?? ""}`}
      aria-label={props.label}
      aria-expanded={props.expanded === undefined ? undefined : props.expanded ? "true" : "false"}
      title={props.label}
      onClick={props.onClick}
      disabled={props.disabled || props.pending}
    >
      <Show when={props.pending} fallback={<Icon name={props.icon} />}>
        <span class="spinner" aria-hidden="true" />
      </Show>
    </button>
  );
}
