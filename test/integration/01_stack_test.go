//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

type healthResponse struct {
	Status string `json:"status"`
}

func TestHeadscaleStackHealthy(t *testing.T) {
	dockerComposeUp(t, "headscale")

	waitForHTTPOK(t, headscaleBaseURL+"/health", 30*time.Second)

	resp, err := http.Get(headscaleBaseURL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	var hr healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
		t.Fatalf("decoding health response: %v", err)
	}

	if hr.Status != "pass" {
		t.Fatalf("expected health status %q, got %q", "pass", hr.Status)
	}
}
