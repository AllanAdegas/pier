# Política de segurança

## Versões com suporte

Corrigimos vulnerabilidades apenas na **versão menor mais recente** (por exemplo, enquanto 0.6.x for a mais nova, só a 0.6.x recebe correções). O Pier está em beta (0.x): para corrigir, atualize para a última versão. O painel avisa quando há uma nova (desligue com `PIER_UPDATE_CHECK=0`).

| Versão | Suporte |
|---|---|
| Última versão menor (`latest`) | Sim |
| Anteriores | Não |

## Como reportar uma vulnerabilidade

**Não abra issue pública.** Use o relato privado do GitHub:

1. Abra a aba **Security** do repositório e clique em **Report a vulnerability** (ou vá direto a `https://github.com/AllanAdegas/pier/security/advisories/new`).
2. Descreva o impacto, os passos para reproduzir, a versão afetada e, se tiver, uma sugestão de correção.

Não inclua segredos reais de produção no relato.

## O que esperar

- Confirmação em até 7 dias e uma avaliação inicial em até 14 dias.
- A correção sai numa versão nova (release automático), junto de um *security advisory* com crédito a quem reportou, se desejar.
- Pedimos divulgação coordenada: aguarde a versão corrigida antes de publicar detalhes.

## Escopo

Está no escopo o código deste repositório: painel (API, interface, MCP), instalador e imagem. Fora do escopo: vulnerabilidades das imagens de terceiros que o Pier implanta, do Docker ou do sistema do servidor, e achados que exijam acesso de administrador já concedido (todo usuário do Pier é administrador; um token `write` equivale a root nos servidores, como documentado).

## Cadeia de suprimentos

Todas as Actions do CI são fixadas por SHA, o CI roda `govulncheck`, `gitleaks` e CodeQL, e as imagens publicadas trazem SBOM e atestado de procedência.

---

**English.** Only the latest minor version is supported. Report vulnerabilities privately through GitHub (Security tab > Report a vulnerability); never in a public issue. Expect an acknowledgement within 7 days and an initial assessment within 14. Fixes ship as a new automatic release plus a security advisory; please coordinate disclosure.
