import { Button } from "./Button";
import { language, LANGUAGES, setLanguage } from "../i18n";

/** Switches the interface to the other language, named in that language. */
export function LanguageSwitch() {
  const other = LANGUAGES.find((entry) => entry.id !== language) ?? LANGUAGES[0];
  return (
    <Button variant="ghost" small icon="globe" onClick={() => setLanguage(other.id)}>
      <span lang={other.id}>{other.label}</span>
    </Button>
  );
}
