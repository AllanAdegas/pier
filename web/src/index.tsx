/* @refresh reload */
import { render } from "solid-js/web";
import "./styles/tokens.css";
import "./styles/base.css";
import "./styles/components.css";
import "./styles/layout.css";
import { App } from "./App";

const root = document.getElementById("root");
if (!root) {
  throw new Error("Elemento #root ausente em index.html");
}
render(() => <App />, root);
