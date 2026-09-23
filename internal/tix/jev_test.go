package tix

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func addJevTestTicket(t *testing.T) int {
	t.Helper()
	newProjects(t, "acme")
	id, err := Add("Add passport stamps", "Choose the stamp behavior", 1, "acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	return int(id)
}

func jevResponseBody(product, ambiguity, scope float64) string {
	return fmt.Sprintf(`{
		"model":"jev-1.13.0",
		"answers":{
			"product_decision_required":{"type":"noul","noul":%g},
			"requirements_ambiguous":{"type":"noul","noul":%g},
			"scope_risk":{"type":"noul","noul":%g}
		},
		"usage":{"input_tokens":742,"output_tokens":104}
	}`, product, ambiguity, scope)
}

func configureJev(t *testing.T, server *httptest.Server, mode JevMode) {
	t.Helper()
	t.Setenv("TIX_JEV_MODE", string(mode))
	t.Setenv("TIX_JEV_BASE_URL", server.URL)
	t.Setenv("JEV_API_KEY", "test-secret-key")
}

func TestJevOffDoesNotMakeHTTPRequest(t *testing.T) {
	setupTestDB(t)
	addJevTestTicket(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	t.Setenv("TIX_JEV_BASE_URL", server.URL)

	ticket, evaluation, err := ClaimNextEvaluated(context.Background(), "acme")
	if err != nil || ticket.Status != "in_progress" || evaluation != nil {
		t.Fatalf("ClaimNextEvaluated = (%+v, %+v, %v)", ticket, evaluation, err)
	}
	if requests.Load() != 0 {
		t.Fatalf("requests = %d, want 0", requests.Load())
	}
}

func TestJevRequestIsStructuredAndAuthenticated(t *testing.T) {
	setupTestDB(t)
	addJevTestTicket(t)
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-secret-key" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, jevResponseBody(.1, .2, .3))
	}))
	defer server.Close()
	configureJev(t, server, JevShadow)

	_, evaluation, err := ClaimNextEvaluated(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if received["model"] != "jev-latest" {
		t.Errorf("model = %#v", received["model"])
	}
	if _, exists := received["prompt"]; exists {
		t.Error("request unexpectedly contains a prose prompt")
	}
	state, ok := received["state"].(map[string]any)
	if !ok {
		t.Fatalf("state = %#v", received["state"])
	}
	ticket, ok := state["ticket"].(map[string]any)
	if !ok || ticket["title"] != "Add passport stamps" || ticket["body"] != "Choose the stamp behavior" {
		t.Fatalf("structured ticket = %#v", state["ticket"])
	}
	if evaluation.Model != "jev-1.13.0" || evaluation.InputTokens != 742 || evaluation.OutputTokens != 104 || evaluation.Judgments.ScopeRisk != .3 || evaluation.Latency <= 0 {
		t.Fatalf("evaluation not captured: %+v", evaluation)
	}
}

func TestJevShadowNeverChangesClaimBehavior(t *testing.T) {
	setupTestDB(t)
	addJevTestTicket(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, jevResponseBody(.99, .99, .99))
	}))
	defer server.Close()
	configureJev(t, server, JevShadow)

	ticket, evaluation, err := ClaimNextEvaluated(context.Background(), "acme")
	if err != nil || ticket.Status != "in_progress" {
		t.Fatalf("shadow claim = (%+v, %v)", ticket, err)
	}
	if evaluation.PolicyDecision != "would_require_human_input" {
		t.Fatalf("policy = %q", evaluation.PolicyDecision)
	}
}

func TestJevEnforceAppliesDeterministicPolicy(t *testing.T) {
	for _, test := range []struct {
		name, response, wantStatus string
	}{
		{"low probabilities claim", jevResponseBody(.1, .2, .3), "in_progress"},
		{"high product decision blocks", jevResponseBody(.95, .1, .1), "blocked"},
	} {
		t.Run(test.name, func(t *testing.T) {
			setupTestDB(t)
			addJevTestTicket(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				io.WriteString(w, test.response)
			}))
			defer server.Close()
			configureJev(t, server, JevEnforce)
			ticket, _, err := ClaimNextEvaluated(context.Background(), "acme")
			if err != nil || ticket.Status != test.wantStatus {
				t.Fatalf("claim = (%+v, %v), want status %s", ticket, err, test.wantStatus)
			}
		})
	}
}

func TestJevFailuresFailOpenWithoutLeakingAPIKey(t *testing.T) {
	for _, test := range []struct {
		name string
		code int
		body string
	}{
		{"authentication", http.StatusUnauthorized, `{"error":"bad key"}`},
		{"validation", http.StatusUnprocessableEntity, `{"error":"bad request"}`},
		{"malformed response", http.StatusOK, `{"model":`},
	} {
		t.Run(test.name, func(t *testing.T) {
			setupTestDB(t)
			addJevTestTicket(t)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(test.code)
				io.WriteString(w, test.body)
			}))
			defer server.Close()
			configureJev(t, server, JevEnforce)
			ticket, evaluation, err := ClaimNextEvaluated(context.Background(), "acme")
			if err != nil || ticket.Status != "in_progress" || evaluation.Err == nil {
				t.Fatalf("failed-open claim = (%+v, %+v, %v)", ticket, evaluation, err)
			}
			if strings.Contains(evaluation.Err.Error(), "test-secret-key") {
				t.Fatalf("error leaked API key: %v", evaluation.Err)
			}
			if requests.Load() != 1 {
				t.Fatalf("requests = %d, want 1 (no retry)", requests.Load())
			}
		})
	}
}

func TestJevRetries429And529(t *testing.T) {
	originalDelay := jevRetryBaseDelay
	jevRetryBaseDelay = time.Millisecond
	t.Cleanup(func() { jevRetryBaseDelay = originalDelay })
	for _, code := range []int{http.StatusTooManyRequests, 529} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			setupTestDB(t)
			addJevTestTicket(t)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if requests.Add(1) < 3 {
					w.WriteHeader(code)
					return
				}
				io.WriteString(w, jevResponseBody(.1, .1, .1))
			}))
			defer server.Close()
			configureJev(t, server, JevShadow)
			if _, evaluation, err := ClaimNextEvaluated(context.Background(), "acme"); err != nil || evaluation.Err != nil {
				t.Fatalf("retry claim = (%+v, %v)", evaluation, err)
			}
			if requests.Load() != 3 {
				t.Fatalf("requests = %d, want 3", requests.Load())
			}
		})
	}
}

func TestJevTimeoutFailsOpenAndTelemetryDistinguishesFailure(t *testing.T) {
	setupTestDB(t)
	id := addJevTestTicket(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(30 * time.Millisecond)
		io.WriteString(w, jevResponseBody(0, 0, 0))
	}))
	defer server.Close()
	cfg := jevConfig{Mode: JevEnforce, APIKey: "secret", BaseURL: server.URL, Model: jevDefaultModel,
		Thresholds: JevJudgments{.8, .8, .8}, Client: &http.Client{Timeout: time.Millisecond}}
	evaluation := evaluateJev(context.Background(), cfg, Ticket{ID: id, Title: "ticket", Project: "acme"}, "ac")
	if evaluation.Err == nil || evaluation.PolicyDecision != "evaluation_failed_open" {
		t.Fatalf("evaluation = %+v", evaluation)
	}
	if err := recordJevEvaluation(id, evaluation); err != nil {
		t.Fatal(err)
	}
	var status string
	var probability any
	var latency int64
	if err := db.QueryRow(`SELECT status, product_decision_required, latency_ms FROM jev_evaluations WHERE ticket_id = ?`, id).Scan(&status, &probability, &latency); err != nil {
		t.Fatal(err)
	}
	if status != "error" || probability != nil || latency < 0 {
		t.Fatalf("telemetry = status %q probability %#v latency %d", status, probability, latency)
	}
}

func TestJevSuccessfulTelemetryCapturesJudgmentsAndUsage(t *testing.T) {
	setupTestDB(t)
	id := addJevTestTicket(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, jevResponseBody(.11, .22, .33))
	}))
	defer server.Close()
	configureJev(t, server, JevShadow)
	if _, _, err := ClaimNextEvaluated(context.Background(), "acme"); err != nil {
		t.Fatal(err)
	}
	var model, mode, status, policy string
	var product, ambiguity, scope float64
	var input, output int
	err := db.QueryRow(`SELECT model, mode, status, policy_decision,
		product_decision_required, requirements_ambiguous, scope_risk, input_tokens, output_tokens
		FROM jev_evaluations WHERE ticket_id = ?`, id).
		Scan(&model, &mode, &status, &policy, &product, &ambiguity, &scope, &input, &output)
	if err != nil {
		t.Fatal(err)
	}
	if model != "jev-1.13.0" || mode != "shadow" || status != "ok" || policy != "claim" || product != .11 || ambiguity != .22 || scope != .33 || input != 742 || output != 104 {
		t.Fatalf("unexpected telemetry: %q %q %q %q %.2f %.2f %.2f %d %d", model, mode, status, policy, product, ambiguity, scope, input, output)
	}
}
