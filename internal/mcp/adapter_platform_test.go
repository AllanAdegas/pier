package mcp

import (
	"context"
	"net/http"
	"testing"
)

func TestToolInvocationContainsAPIPanics(t *testing.T) {
	invoke := invoker{api: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("dummy-secret-must-not-escape") })}
	result, err := invoke.call(context.Background(), "Bearer dummy", apiRequest{Method: "GET", Path: "/api/v1/applications"})
	if err != nil || result == nil || !result.IsError {
		t.Fatalf("result %+v err %v", result, err)
	}
}
