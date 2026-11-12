package application

import (
	"context"
	"path"
	"strings"

	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

// RailpackVersion is the exact Railpack release Pier installs on servers.
// Railpack (https://github.com/railwayapp/railpack) is MIT licensed.
const RailpackVersion = "0.43.0"

// railpackFrontend is the BuildKit frontend of the same release, pinned by
// digest: the plan must be built by the frontend version that generated it.
const railpackFrontend = "ghcr.io/railwayapp/railpack-frontend:v0.43.0@sha256:355ef61fae6f2551103cd8b10c710cb07167dd353d561f15c69b8d7ac1c676cc"

// railpackAsset is one release archive and the checksums Pier trusts for it:
// the archive (verified before extraction) and the extracted binary
// (verified before every use).
type railpackAsset struct {
	archive       string
	archiveSHA256 string
	binarySHA256  string
}

var railpackAssets = map[string]railpackAsset{
	"x86_64": {
		archive:       "railpack-v0.43.0-x86_64-unknown-linux-musl.tar.gz",
		archiveSHA256: "248a8f60f36854aabed6124113631078a802de3461660855fc4de9c26065c1fb",
		binarySHA256:  "3e9a8477b2c7cd0bfb46ebdb0ca5f3153329f4f01cec263dbb6ea9b4679a3af9",
	},
	"aarch64": {
		archive:       "railpack-v0.43.0-arm64-unknown-linux-musl.tar.gz",
		archiveSHA256: "732ece14eb4f4676a43ebba19431f954edf7b130acb76159ef0f2ff86ac42ac9",
		binarySHA256:  "d70e585b1cc0c9782fe2b09b3b84c1528833cc532d3cb0766201b8feb32b042f",
	},
}

// railpackInstallScript downloads, verifies and extracts inside a disposable
// container, so the server needs neither curl nor tar. A checksum mismatch
// stops before anything is extracted.
const railpackInstallScript = `set -eu
rm -rf /out/.download
mkdir -p /out/.download
cd /out/.download
wget -q -O railpack.tar.gz "$PIER_RAILPACK_URL"
echo "$PIER_RAILPACK_SHA256  railpack.tar.gz" | sha256sum -c -
tar -xzf railpack.tar.gz railpack
chmod 0755 railpack
mv -f railpack /out/railpack
cd /out
rm -rf /out/.download`

func railpackDir(dataDir string) string {
	return path.Join(dataDir, "tools", "railpack", "v"+RailpackVersion)
}

func railpackURL(asset railpackAsset) string {
	return "https://github.com/railwayapp/railpack/releases/download/v" + RailpackVersion + "/" + asset.archive
}

// ensureRailpack returns the path of a verified Railpack binary on the
// server, installing the pinned release the first time it is needed.
func (j *job) ensureRailpack(ctx context.Context) (string, error) {
	asset, err := j.railpackAsset(ctx)
	if err != nil {
		return "", err
	}
	dir := railpackDir(j.server.DataDir)
	binary := path.Join(dir, "railpack")
	if j.railpackVerified(ctx, binary, asset) {
		return binary, nil
	}
	j.log.Info("Instalando o Railpack %s no servidor (checksum verificado).", RailpackVersion)
	if _, err := j.ex.Run(ctx, shell.Join("mkdir", "-p", "--", dir)); err != nil {
		return "", err
	}
	install := shell.Join("docker", "run", "--rm", "-v", dir+":/out",
		"-e", "PIER_RAILPACK_URL="+railpackURL(asset), "-e", "PIER_RAILPACK_SHA256="+asset.archiveSHA256,
		"--entrypoint", "sh", gitImage, "-c", railpackInstallScript)
	if err := j.ex.Stream(ctx, install, j.log.Output); err != nil {
		return "", i18n.Errorf("não foi possível instalar o Railpack %s no servidor: %w", RailpackVersion, err)
	}
	if !j.railpackVerified(ctx, binary, asset) {
		return "", i18n.Errorf("o binário do Railpack instalado não confere com o checksum esperado; o deploy foi interrompido")
	}
	return binary, nil
}

func (j *job) railpackAsset(ctx context.Context) (railpackAsset, error) {
	machine, err := j.ex.Run(ctx, "uname -m")
	if err != nil {
		return railpackAsset{}, err
	}
	machine = strings.TrimSpace(machine)
	if machine == "arm64" {
		machine = "aarch64"
	}
	asset, found := railpackAssets[machine]
	if !found {
		return railpackAsset{}, i18n.Errorf("o Railpack não tem versão para a arquitetura %s do servidor; use o build pack Dockerfile", machine)
	}
	return asset, nil
}

// railpackVerified reports whether the binary exists and matches the pinned
// checksum, so a tampered or truncated file is replaced instead of executed.
func (j *job) railpackVerified(ctx context.Context, binary string, asset railpackAsset) bool {
	output, err := j.ex.Run(ctx, shell.Join("sha256sum", "--", binary))
	if err != nil {
		return false
	}
	fields := strings.Fields(output)
	return len(fields) > 0 && fields[0] == asset.binarySHA256
}
