package mcp

import (
	"context"
	"net/http"
	"testing"
)

func TestDeployTagSendsTheRefToTheDeployRoute(t *testing.T) {
	request, err := deployTag(deployTagInput{ID: "app1", Ref: " v1.2.3 "})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := request.Body.(map[string]string)
	if request.Method != http.MethodPost || request.Path != "/api/v1/applications/app1/deploy" || body["ref"] != "v1.2.3" {
		t.Errorf("request = %+v", request)
	}
	for _, input := range []deployTagInput{{ID: "app1"}, {ID: "app1", Ref: "v1 2"}, {ID: "../x", Ref: "v1"}} {
		if _, err := deployTag(input); err == nil {
			t.Errorf("%+v accepted", input)
		}
	}
}

func TestPromoteNamesTheTarget(t *testing.T) {
	request, err := promote(promoteInput{ID: "staging", TargetApplicationID: "production"})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := request.Body.(map[string]string)
	if request.Path != "/api/v1/applications/staging/promote" || body["target_application_id"] != "production" {
		t.Errorf("request = %+v", request)
	}
	for _, input := range []promoteInput{{ID: "staging"}, {ID: "staging", TargetApplicationID: "a/b"}} {
		if _, err := promote(input); err == nil {
			t.Errorf("%+v accepted", input)
		}
	}
}

func TestReleaseToolsAreListedAsNonDestructiveDeployTools(t *testing.T) {
	server, _ := startServer(t)
	tools, err := connect(t, server, deployToken).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, tool := range tools.Tools {
		found[tool.Name] = true
	}
	if !found["deploy_application_tag"] || !found["promote_application"] {
		t.Errorf("tools = %v", found)
	}
}
