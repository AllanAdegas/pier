package stack

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"pier/internal/gitprovider"
)

type selectedStackCredentials struct {
	id      string
	revoked bool
}

func (p *selectedStackCredentials) WithCredentials(_ context.Context, kind, id string, callback func(string, string) error) error {
	if kind != "stack" || id != p.id {
		return gitprovider.ErrNoSelection
	}
	if p.revoked {
		return errors.New("installation revoked")
	}
	return callback("https://github.com/acme/shop.git", "ephemeral-stack-token")
}

func TestGitStackDeployResolvesSelectedInstallationAtClone(t *testing.T) {
	service, fake := newGitService(t)
	input := gitInput()
	input.Git.Repository = "https://github.com/acme/shop.git"
	resource, err := service.Create(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	service.SetGitProvider(&selectedStackCredentials{id: resource.ID})
	seedClone(fake, resource, DefaultComposePath, gitCompose)
	log, err := deployAndWait(t, service, resource.ID)
	if err != nil {
		t.Fatal(err, log)
	}
	private := string(fake.Files["/data/pier/builds/stack-"+resource.ID+"/clone.env"])
	expected := base64.StdEncoding.EncodeToString([]byte("x-access-token:ephemeral-stack-token"))
	if !strings.Contains(private, "Authorization: Basic "+expected) || !strings.Contains(private, "PIER_REPOSITORY=https://github.com/acme/shop.git") {
		t.Fatal("stack clone omitted installation credentials")
	}
	if strings.Contains(log, "ephemeral-stack-token") {
		t.Fatal("secret in progress log")
	}
	for _, c := range fake.Commands {
		if strings.Contains(c, "ephemeral-stack-token") {
			t.Fatal("secret in argv")
		}
	}
	if !fake.Ran(" up ") {
		t.Fatal("stack did not execute full deployment")
	}
	saved, err := service.Get(t.Context(), resource.ID)
	if err != nil || saved.Git.LastCommit != gitCommit {
		t.Fatal(saved, err)
	}
}
func TestGitStackSelectedInstallationFailureDoesNotRunPublicClone(t *testing.T) {
	service, fake := newGitService(t)
	input := gitInput()
	input.Git.Repository = "https://github.com/acme/shop.git"
	resource, err := service.Create(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	service.SetGitProvider(&selectedStackCredentials{id: resource.ID, revoked: true})
	if _, err = deployAndWait(t, service, resource.ID); err == nil {
		t.Fatal("revocation ignored")
	}
	if fake.Ran("git clone --depth") {
		t.Fatal("unauthenticated fallback used")
	}
}
