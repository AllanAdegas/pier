package provision

import (
	"regexp"
	"strings"
	"time"

	"pier/internal/platform/apperr"
)

// Machine statuses, in the order provisioning goes through them.
const (
	StatusCreating   = "creating"
	StatusWaitingSSH = "waiting_ssh"
	StatusInstalling = "installing"
	StatusReady      = "ready"
	StatusFailed     = "failed"
)

var (
	machineName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	catalogID   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	tokenText   = regexp.MustCompile(`^[\x21-\x7e]{16,256}$`)
	publicKey   = regexp.MustCompile(`^ssh-(ed25519|rsa) [A-Za-z0-9+/=]+$`)
)

// Credential is a cloud account Pier can create machines in. The token
// itself is sealed in the database and never leaves the backend.
type Credential struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	Name      string    `json:"name"`
	KeyID     string    `json:"key_id"`
	CreatedAt time.Time `json:"created_at"`
}

// CredentialInput registers a cloud account.
type CredentialInput struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	Token    string `json:"token"`
}

func (input CredentialInput) validate() (CredentialInput, error) {
	input.Name, input.Token = strings.TrimSpace(input.Name), strings.TrimSpace(input.Token)
	fields := apperr.Fields{}
	if input.Provider != ProviderHetzner && input.Provider != ProviderDigitalOcean {
		fields.Add("provider", "Escolha Hetzner Cloud ou DigitalOcean.")
	}
	if input.Name == "" || len(input.Name) > 64 {
		fields.Add("name", "Informe um nome de até 64 caracteres.")
	}
	if !tokenText.MatchString(input.Token) {
		fields.Add("token", "Cole o token de API do provedor (sem espaços).")
	}
	return input, fields.Err()
}

// Machine is a server Pier created at a provider.
type Machine struct {
	ID            string    `json:"id"`
	CredentialID  string    `json:"credential_id"`
	Provider      string    `json:"provider"`
	Name          string    `json:"name"`
	Region        string    `json:"region"`
	Size          string    `json:"size"`
	Image         string    `json:"image"`
	InstallDocker bool      `json:"install_docker"`
	ExternalID    string    `json:"external_id"`
	IPv4          string    `json:"ipv4"`
	ServerID      string    `json:"server_id"`
	Status        string    `json:"status"`
	Error         string    `json:"error"`
	CreatedAt     time.Time `json:"created_at"`
}

// MachineInput asks for a new machine.
type MachineInput struct {
	CredentialID  string `json:"credential_id"`
	Name          string `json:"name"`
	Region        string `json:"region"`
	Size          string `json:"size"`
	Image         string `json:"image"`
	InstallDocker bool   `json:"install_docker"`
}

func (input MachineInput) validate() (MachineInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	fields := apperr.Fields{}
	if input.CredentialID == "" {
		fields.Add("credential_id", "Escolha a conta do provedor.")
	}
	if !machineName.MatchString(input.Name) {
		fields.Add("name", "Use letras minúsculas, números e hífens (até 63), começando e terminando com letra ou número.")
	}
	for field, value := range map[string]string{"region": input.Region, "size": input.Size, "image": input.Image} {
		if !catalogID.MatchString(value) {
			fields.Add(field, "Escolha uma opção da lista.")
		}
	}
	return input, fields.Err()
}

// cloudInit is the user data that authorizes Pier's key for root. The key
// is validated to a single "type base64" pair, so it cannot break the YAML.
func cloudInit(authorizedKey string) (string, error) {
	key := authorizedKeyOnly(authorizedKey)
	if !publicKey.MatchString(key) {
		return "", apperr.Invalid("Chave pública SSH inválida.")
	}
	return "#cloud-config\ndisable_root: false\nssh_pwauth: false\nssh_authorized_keys:\n  - \"" + key + "\"\n", nil
}

// authorizedKeyOnly drops the comment of an authorized_keys line.
func authorizedKeyOnly(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return strings.TrimSpace(line)
	}
	return fields[0] + " " + fields[1]
}
