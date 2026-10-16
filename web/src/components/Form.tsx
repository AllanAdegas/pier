import { createUniqueId, For, Show, splitProps, type JSX } from "solid-js";

interface FieldShell {
  label: string;
  hint?: string;
  error?: string;
}

/** Label, hint and error wrapped around a control, wired for accessibility. */
function Field(props: FieldShell & { id: string; children: JSX.Element }) {
  return (
    <div class="field">
      <label class="field-label" for={props.id}>
        {props.label}
      </label>
      {props.children}
      <Show when={props.error} fallback={<Show when={props.hint}><span class="field-hint" id={`${props.id}-hint`}>{props.hint}</span></Show>}>
        <span class="field-error" id={`${props.id}-error`} role="alert">
          {props.error}
        </span>
      </Show>
    </div>
  );
}

function describedBy(id: string, props: FieldShell): string | undefined {
  if (props.error) {
    return `${id}-error`;
  }
  return props.hint ? `${id}-hint` : undefined;
}

type InputProps = FieldShell &
  Omit<JSX.InputHTMLAttributes<HTMLInputElement>, "onInput" | "value"> & {
    value: string | number;
    onValue: (value: string) => void;
    mono?: boolean;
  };

export function TextInput(props: InputProps) {
  const id = createUniqueId();
  const [own, rest] = splitProps(props, ["label", "hint", "error", "value", "onValue", "mono", "class"]);
  return (
    <Field id={id} label={own.label} hint={own.hint} error={own.error}>
      <input
        {...rest}
        id={id}
        class={`input ${own.mono ? "mono" : ""}`}
        value={own.value}
        onInput={(event) => own.onValue(event.currentTarget.value)}
        aria-invalid={own.error ? "true" : undefined}
        aria-describedby={describedBy(id, own)}
      />
    </Field>
  );
}

type NumberProps = FieldShell & {
  value: number;
  onValue: (value: number) => void;
  min?: number;
  max?: number;
  placeholder?: string;
};

/** Numeric input; an empty box means zero ("not set"). */
export function NumberInput(props: NumberProps) {
  const id = createUniqueId();
  return (
    <Field id={id} label={props.label} hint={props.hint} error={props.error}>
      <input
        id={id}
        class="input"
        type="number"
        inputmode="numeric"
        min={props.min}
        max={props.max}
        placeholder={props.placeholder}
        value={props.value === 0 && props.placeholder ? "" : props.value}
        onInput={(event) => props.onValue(Number.parseInt(event.currentTarget.value, 10) || 0)}
        aria-invalid={props.error ? "true" : undefined}
        aria-describedby={describedBy(id, props)}
      />
    </Field>
  );
}

type TextAreaProps = FieldShell & {
  value: string;
  onValue: (value: string) => void;
  rows?: number;
  mono?: boolean;
  placeholder?: string;
  spellcheck?: boolean;
};

export function TextArea(props: TextAreaProps) {
  const id = createUniqueId();
  return (
    <Field id={id} label={props.label} hint={props.hint} error={props.error}>
      <textarea
        id={id}
        class={`textarea ${props.mono ? "mono" : ""}`}
        rows={props.rows ?? 5}
        placeholder={props.placeholder}
        spellcheck={props.spellcheck ?? !props.mono}
        value={props.value}
        onInput={(event) => props.onValue(event.currentTarget.value)}
        aria-invalid={props.error ? "true" : undefined}
        aria-describedby={describedBy(id, props)}
      />
    </Field>
  );
}

export interface Option {
  value: string;
  label: string;
}

type SelectProps = FieldShell & {
  value: string;
  onValue: (value: string) => void;
  options: Option[];
  /** Label of the empty option; omit to force a choice. */
  placeholder?: string;
  disabled?: boolean;
};

export function Select(props: SelectProps) {
  const id = createUniqueId();
  return (
    <Field id={id} label={props.label} hint={props.hint} error={props.error}>
      <select
        id={id}
        class="select"
        value={props.value}
        disabled={props.disabled}
        onChange={(event) => props.onValue(event.currentTarget.value)}
        aria-invalid={props.error ? "true" : undefined}
        aria-describedby={describedBy(id, props)}
      >
        <Show when={props.placeholder !== undefined}>
          <option value="">{props.placeholder}</option>
        </Show>
        <For each={props.options}>
          {(option) => (
            <option value={option.value} selected={option.value === props.value}>
              {option.label}
            </option>
          )}
        </For>
      </select>
    </Field>
  );
}

export function Checkbox(props: {
  label: string;
  hint?: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
}) {
  return (
    <label class="check">
      <input
        type="checkbox"
        checked={props.checked}
        disabled={props.disabled}
        onChange={(event) => props.onChange(event.currentTarget.checked)}
      />
      <span class="stack-sm" style={{ gap: "2px" }}>
        <span class="field-label">{props.label}</span>
        <Show when={props.hint}>
          <span class="field-hint">{props.hint}</span>
        </Show>
      </span>
    </label>
  );
}

/** Edits a list of short strings (domains, port mappings), one per line. */
export function LinesInput(props: FieldShell & { value: string[]; onValue: (value: string[]) => void; placeholder?: string }) {
  return (
    <TextArea
      label={props.label}
      hint={props.hint}
      error={props.error}
      mono
      rows={Math.max(2, props.value.length + 1)}
      placeholder={props.placeholder}
      value={props.value.join("\n")}
      onValue={(text) => props.onValue(text.split("\n"))}
    />
  );
}
