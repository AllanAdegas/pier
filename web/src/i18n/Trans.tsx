import { For, type JSX } from "solid-js";
import { t } from "./index";

/**
 * A translated sentence with elements inside it, such as a link or a code
 * span: each {name} in the text is replaced by the element given for it, so
 * the translation can move them around.
 */
export function Trans(props: { text: string; values: Record<string, JSX.Element> }) {
  const pieces = t(props.text).split(/((?<!\$)\{\w+\})/);
  return (
    <For each={pieces}>
      {(piece) => {
        const name = /^\{(\w+)\}$/.exec(piece)?.[1];
        return name && name in props.values ? props.values[name] : piece;
      }}
    </For>
  );
}
