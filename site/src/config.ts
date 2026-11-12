/**
 * Facts about the release that the site prints. The version is read from the
 * server's own source, so the install commands never drift from the code.
 */
export const repository = "https://github.com/AllanAdegas/pier";
export const cliRepository = "https://github.com/AllanAdegas/pier-cli";

const versionFile = new URL("../../internal/version/version.go", import.meta.url);

export function parseVersion(source: string): string {
  const match = source.match(/Number\s*=\s*"(\d+\.\d+\.\d+)"/);
  if (!match) throw new Error("version.go: constante Number não encontrada");
  return match[1];
}

export async function readVersion(): Promise<string> {
  return parseVersion(await Deno.readTextFile(versionFile));
}

/** Values substituted for {{name}} in every page. */
export function placeholders(version: string): Record<string, string> {
  return {
    version,
    tag: `v${version}`,
    repo: repository,
    cliRepo: cliRepository,
  };
}

export function fillPlaceholders(text: string, values: Record<string, string>): string {
  return text.replace(/\{\{(\w+)\}\}/g, (whole, name: string) => {
    const value = values[name];
    if (value === undefined) throw new Error(`marcador desconhecido: ${whole}`);
    return value;
  });
}
