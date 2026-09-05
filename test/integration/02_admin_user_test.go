//go:build integration

package integration

import (
	"encoding/json"
	"testing"
)

type headscaleUser struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func TestAdminUserExists(t *testing.T) {
	dockerComposeUp(t, "headscale")

	out := headscaleExec(t, "users", "list", "-o", "json")

	var users []headscaleUser
	if err := json.Unmarshal([]byte(out), &users); err != nil {
		t.Fatalf("decoding users list %q: %v", out, err)
	}

	for _, u := range users {
		if u.Name == "marcelo" {
			return
		}
	}

	t.Fatalf("expected user %q to exist, got %v", "marcelo", users)
}
