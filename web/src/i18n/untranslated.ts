// Portuguese that is shown on purpose without translation. The guard in
// hardcoded.test.ts accepts exactly these lines; every entry says why.
export const UNTRANSLATED: { file: string; text: string; reason: string }[] = [
  {
    file: "i18n/index.ts",
    text: `{ id: "pt-BR", label: "Português" },`,
    reason: "A language is always named in itself, so a Portuguese reader finds it in an English interface.",
  },
];
