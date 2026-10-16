// Package kube is a small client for the Kubernetes API. It is the
// anti-corruption layer for clusters: the rest of Pier never builds API
// paths or parses Kubernetes responses.
//
// It speaks plain REST with net/http instead of depending on client-go:
// Pier needs discovery, generic CRUD, server-side apply and pod logs, which
// is a few hundred lines, not a framework.
package kube

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is everything needed to reach and authenticate to one cluster.
type Config struct {
	Server string
	// CAData is the PEM bundle that signs the API server certificate.
	CAData []byte
	// Insecure skips server certificate verification (kubeconfig opt-in).
	Insecure   bool
	ClientCert []byte
	ClientKey  []byte
	Token      string
	Username   string
	Password   string
}

// ErrUnsupportedAuth means the kubeconfig authenticates in a way Pier cannot
// reproduce on its own (an external command or files on someone's disk).
var ErrUnsupportedAuth = errors.New("autenticação do kubeconfig não suportada")

type kubeconfigFile struct {
	CurrentContext string `yaml:"current-context"`
	Clusters       []struct {
		Name    string `yaml:"name"`
		Cluster struct {
			Server                   string `yaml:"server"`
			CertificateAuthority     string `yaml:"certificate-authority"`
			CertificateAuthorityData string `yaml:"certificate-authority-data"`
			InsecureSkipTLSVerify    bool   `yaml:"insecure-skip-tls-verify"`
		} `yaml:"cluster"`
	} `yaml:"clusters"`
	Users []struct {
		Name string `yaml:"name"`
		User struct {
			ClientCertificate     string `yaml:"client-certificate"`
			ClientCertificateData string `yaml:"client-certificate-data"`
			ClientKey             string `yaml:"client-key"`
			ClientKeyData         string `yaml:"client-key-data"`
			Token                 string `yaml:"token"`
			TokenFile             string `yaml:"tokenFile"`
			Username              string `yaml:"username"`
			Password              string `yaml:"password"`
			Exec                  any    `yaml:"exec"`
			AuthProvider          any    `yaml:"auth-provider"`
		} `yaml:"user"`
	} `yaml:"users"`
	Contexts []struct {
		Name    string `yaml:"name"`
		Context struct {
			Cluster string `yaml:"cluster"`
			User    string `yaml:"user"`
		} `yaml:"context"`
	} `yaml:"contexts"`
}

// ParseKubeconfig extracts the connection settings of one context. An empty
// contextName selects the file's current-context.
//
// Only self-contained credentials are accepted: embedded certificates, a
// bearer token or basic auth. References to local files and exec plugins
// (gcloud, aws, oidc-login) are rejected with ErrUnsupportedAuth, because the
// kubeconfig is stored and used on the Pier host, where they do not exist.
func ParseKubeconfig(data []byte, contextName string) (Config, error) {
	var file kubeconfigFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return Config{}, fmt.Errorf("o kubeconfig não é um YAML válido: %w", err)
	}
	if contextName == "" {
		contextName = file.CurrentContext
	}
	if contextName == "" && len(file.Contexts) == 1 {
		contextName = file.Contexts[0].Name
	}
	clusterName, userName := "", ""
	for _, context := range file.Contexts {
		if context.Name == contextName {
			clusterName, userName = context.Context.Cluster, context.Context.User
		}
	}
	if clusterName == "" {
		return Config{}, fmt.Errorf("contexto %q não encontrado no kubeconfig", contextName)
	}

	var config Config
	for _, entry := range file.Clusters {
		if entry.Name != clusterName {
			continue
		}
		if entry.Cluster.CertificateAuthority != "" && entry.Cluster.CertificateAuthorityData == "" {
			return Config{}, fmt.Errorf("%w: o certificado da CA aponta para um arquivo local; "+
				"gere o kubeconfig com os dados embutidos (kubectl config view --flatten --minify)", ErrUnsupportedAuth)
		}
		caData, err := decode(entry.Cluster.CertificateAuthorityData, "certificate-authority-data")
		if err != nil {
			return Config{}, err
		}
		config.Server = strings.TrimRight(entry.Cluster.Server, "/")
		config.CAData = caData
		config.Insecure = entry.Cluster.InsecureSkipTLSVerify
	}
	if parsed, err := url.Parse(config.Server); err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return Config{}, fmt.Errorf("endereço do cluster ausente ou inválido no kubeconfig")
	}

	for _, entry := range file.Users {
		if entry.Name != userName {
			continue
		}
		user := entry.User
		if user.Exec != nil || user.AuthProvider != nil {
			return Config{}, fmt.Errorf("%w: o usuário autentica por um comando externo (exec/auth-provider), "+
				"comum em GKE, EKS e AKS. Crie uma ServiceAccount com token e use um kubeconfig com esse token", ErrUnsupportedAuth)
		}
		if user.TokenFile != "" || (user.ClientCertificate != "" && user.ClientCertificateData == "") ||
			(user.ClientKey != "" && user.ClientKeyData == "") {
			return Config{}, fmt.Errorf("%w: as credenciais apontam para arquivos locais; "+
				"gere o kubeconfig com os dados embutidos (kubectl config view --flatten --minify)", ErrUnsupportedAuth)
		}
		cert, err := decode(user.ClientCertificateData, "client-certificate-data")
		if err != nil {
			return Config{}, err
		}
		key, err := decode(user.ClientKeyData, "client-key-data")
		if err != nil {
			return Config{}, err
		}
		config.ClientCert, config.ClientKey = cert, key
		config.Token, config.Username, config.Password = user.Token, user.Username, user.Password
	}
	if len(config.ClientCert) == 0 && config.Token == "" && config.Username == "" {
		return Config{}, fmt.Errorf("o kubeconfig não traz credenciais (certificado de cliente, token ou usuário e senha)")
	}
	return config, nil
}

// Contexts lists the context names of a kubeconfig and its current one.
func Contexts(data []byte) (names []string, current string, err error) {
	var file kubeconfigFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, "", fmt.Errorf("o kubeconfig não é um YAML válido: %w", err)
	}
	for _, context := range file.Contexts {
		names = append(names, context.Name)
	}
	return names, file.CurrentContext, nil
}

// RewriteServer returns the kubeconfig with every cluster pointing at a new
// API address. K3s writes its kubeconfig for 127.0.0.1 inside the node.
func RewriteServer(data []byte, server string) ([]byte, error) {
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("o kubeconfig não é um YAML válido: %w", err)
	}
	clusters, _ := document["clusters"].([]any)
	for _, entry := range clusters {
		item, _ := entry.(map[string]any)
		if cluster, ok := item["cluster"].(map[string]any); ok {
			cluster["server"] = server
		}
	}
	return yaml.Marshal(document)
}

func decode(encoded, field string) ([]byte, error) {
	if encoded == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("o campo %s do kubeconfig não é base64 válido", field)
	}
	return decoded, nil
}

// Render writes the settings back as a self-contained, single-context
// kubeconfig. Storing this instead of the original file guarantees that no
// credential for another cluster is kept along.
func (c Config) Render() ([]byte, error) {
	cluster := map[string]any{"server": c.Server}
	if len(c.CAData) > 0 {
		cluster["certificate-authority-data"] = base64.StdEncoding.EncodeToString(c.CAData)
	}
	if c.Insecure {
		cluster["insecure-skip-tls-verify"] = true
	}
	user := map[string]any{}
	if len(c.ClientCert) > 0 {
		user["client-certificate-data"] = base64.StdEncoding.EncodeToString(c.ClientCert)
		user["client-key-data"] = base64.StdEncoding.EncodeToString(c.ClientKey)
	}
	if c.Token != "" {
		user["token"] = c.Token
	}
	if c.Username != "" {
		user["username"], user["password"] = c.Username, c.Password
	}
	return yaml.Marshal(map[string]any{
		"apiVersion": "v1", "kind": "Config", "current-context": "pier",
		"clusters": []any{map[string]any{"name": "pier", "cluster": cluster}},
		"users":    []any{map[string]any{"name": "pier", "user": user}},
		"contexts": []any{map[string]any{"name": "pier", "context": map[string]any{"cluster": "pier", "user": "pier"}}},
	})
}
