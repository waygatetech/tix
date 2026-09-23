package tix

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	jev "github.com/waygatetech/jev-go"
)

const (
	jevDefaultModel     = "jev-latest"
	jevDefaultThreshold = 0.8
)

type JevMode string

const (
	JevOff     JevMode = "off"
	JevShadow  JevMode = "shadow"
	JevEnforce JevMode = "enforce"
)

// jevQuestions is the single place where tix defines the semantic judgments
// it asks Jev to make. Policy thresholds intentionally live elsewhere.
var jevQuestions = map[string]jev.Question{
	"product_decision_required": {
		Type:         "noul",
		Instructions: "Is implementing this ticket likely to require an unresolved product decision rather than merely an engineering implementation decision?",
		Criteria: map[string]string{
			"true":  "The agent would need to choose or change user-facing behavior, requirements, or product semantics not already established by the ticket or project context.",
			"false": "The ticket is sufficiently specified or remaining choices are routine engineering implementation decisions.",
		},
	},
	"requirements_ambiguous": {
		Type:         "noul",
		Instructions: "Are the ticket requirements ambiguous enough that materially different reasonable implementations are possible and clarification would prevent likely rework?",
		Criteria: map[string]string{
			"true":  "Important requirements are genuinely unresolved and choosing among reasonable interpretations could materially affect the result.",
			"false": "The requirements are sufficiently clear; ordinary engineering discretion does not count as ambiguity.",
		},
	},
	"scope_risk": {
		Type:         "noul",
		Instructions: "Is there substantial risk that implementing this ticket as written will require work materially beyond its stated scope?",
		Criteria: map[string]string{
			"true":  "The likely implementation requires meaningful additional systems, behavior, or requirements beyond the ticket.",
			"false": "The likely implementation stays within the ticket; incidental implementation work does not count as scope expansion.",
		},
	},
}

// jevState is the state tix sends Jev when judging a ticket.
type jevState struct {
	Ticket struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Body  string `json:"body"`
	} `json:"ticket"`
	Project struct {
		Constraints []string `json:"constraints"`
	} `json:"project"`
}

type JevJudgments struct {
	ProductDecisionRequired float64 `json:"product_decision_required"`
	RequirementsAmbiguous   float64 `json:"requirements_ambiguous"`
	ScopeRisk               float64 `json:"scope_risk"`
}

type JevEvaluation struct {
	Mode           JevMode       `json:"mode"`
	Model          string        `json:"model,omitempty"`
	Judgments      JevJudgments  `json:"judgments"`
	PolicyDecision string        `json:"policy_decision"`
	Latency        time.Duration `json:"-"`
	InputTokens    int           `json:"input_tokens,omitempty"`
	OutputTokens   int           `json:"output_tokens,omitempty"`
	HTTPStatus     int           `json:"http_status,omitempty"`
	Err            error         `json:"-"`
}

type jevConfig struct {
	Mode       JevMode
	APIKey     string
	BaseURL    string
	Model      string
	Thresholds JevJudgments
	Client     *http.Client
}

// jevRetryBaseDelay is a var, not a const, so tests can shrink it.
var jevRetryBaseDelay = 200 * time.Millisecond

func loadJevConfig() (jevConfig, error) {
	mode := JevMode(strings.ToLower(strings.TrimSpace(os.Getenv("TIX_JEV_MODE"))))
	if mode == "" {
		mode = JevOff
	}
	if mode != JevOff && mode != JevShadow && mode != JevEnforce {
		return jevConfig{}, fmt.Errorf("invalid TIX_JEV_MODE %q (want off, shadow, or enforce)", mode)
	}
	cfg := jevConfig{
		Mode:    mode,
		APIKey:  os.Getenv("JEV_API_KEY"),
		BaseURL: envOr("TIX_JEV_BASE_URL", jev.DefaultBaseURL),
		Model:   envOr("TIX_JEV_MODEL", jevDefaultModel),
	}
	var err error
	if cfg.Thresholds.ProductDecisionRequired, err = threshold("TIX_JEV_PRODUCT_DECISION_THRESHOLD"); err != nil {
		return jevConfig{}, err
	}
	if cfg.Thresholds.RequirementsAmbiguous, err = threshold("TIX_JEV_REQUIREMENTS_AMBIGUOUS_THRESHOLD"); err != nil {
		return jevConfig{}, err
	}
	if cfg.Thresholds.ScopeRisk, err = threshold("TIX_JEV_SCOPE_RISK_THRESHOLD"); err != nil {
		return jevConfig{}, err
	}
	return cfg, nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func threshold(name string) (float64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return jevDefaultThreshold, nil
	}
	n, err := strconv.ParseFloat(value, 64)
	if err != nil || n < 0 || n > 1 {
		return 0, fmt.Errorf("%s must be a number from 0 to 1", name)
	}
	return n, nil
}

func evaluateJev(ctx context.Context, cfg jevConfig, ticket Ticket, prefix string) (evaluation JevEvaluation) {
	evaluation = JevEvaluation{Mode: cfg.Mode, PolicyDecision: "claim"}
	started := time.Now()
	defer func() { evaluation.Latency = time.Since(started) }()

	if strings.TrimSpace(cfg.APIKey) == "" {
		evaluation.Err = errors.New("JEV_API_KEY is not set")
		evaluation.PolicyDecision = "evaluation_failed_open"
		return evaluation
	}

	var state jevState
	state.Ticket.ID = FormatID(prefix, ticket.ID)
	state.Ticket.Title = ticket.Title
	state.Ticket.Body = ticket.Body
	state.Project.Constraints = []string{}
	request := jev.Request{Model: cfg.Model, State: state, Questions: jevQuestions}

	client := jev.NewClient(cfg.APIKey)
	client.BaseURL = cfg.BaseURL
	client.RetryBaseDelay = jevRetryBaseDelay
	if cfg.Client != nil {
		client.HTTPClient = cfg.Client
	}
	response, err := client.Evaluate(ctx, request)
	evaluation.HTTPStatus = response.HTTPStatus
	if err != nil {
		evaluation.Err = err
		evaluation.PolicyDecision = "evaluation_failed_open"
		return evaluation
	}
	if err := populateEvaluation(&evaluation, response); err != nil {
		evaluation.Err = err
		evaluation.PolicyDecision = "evaluation_failed_open"
		return evaluation
	}
	if requiresHumanInput(evaluation.Judgments, cfg.Thresholds) {
		if cfg.Mode == JevShadow {
			evaluation.PolicyDecision = "would_require_human_input"
		} else {
			evaluation.PolicyDecision = "require_human_input"
		}
	}
	return evaluation
}

func populateEvaluation(e *JevEvaluation, response jev.Response) error {
	for _, name := range []string{"product_decision_required", "requirements_ambiguous", "scope_risk"} {
		if response.Answers[name].Type != "noul" {
			return fmt.Errorf("malformed Jev response: %s is not a noul answer", name)
		}
	}
	values := []*float64{
		response.Answers["product_decision_required"].Noul,
		response.Answers["requirements_ambiguous"].Noul,
		response.Answers["scope_risk"].Noul,
	}
	for i, value := range values {
		if value == nil || *value < 0 || *value > 1 {
			return fmt.Errorf("malformed Jev response: missing or invalid noul answer %d", i+1)
		}
	}
	e.Model = response.Model
	e.Judgments = JevJudgments{*values[0], *values[1], *values[2]}
	e.InputTokens = response.Usage.InputTokens
	e.OutputTokens = response.Usage.OutputTokens
	return nil
}

func requiresHumanInput(got, thresholds JevJudgments) bool {
	return got.ProductDecisionRequired >= thresholds.ProductDecisionRequired ||
		got.RequirementsAmbiguous >= thresholds.RequirementsAmbiguous ||
		got.ScopeRisk >= thresholds.ScopeRisk
}

func recordJevEvaluation(ticketID int, e JevEvaluation) error {
	status := "ok"
	errText := ""
	var product, ambiguity, scope any = e.Judgments.ProductDecisionRequired, e.Judgments.RequirementsAmbiguous, e.Judgments.ScopeRisk
	if e.Err != nil {
		status = "error"
		if e.HTTPStatus != 0 {
			status = fmt.Sprintf("http_%d", e.HTTPStatus)
		}
		errText = e.Err.Error()
		product, ambiguity, scope = nil, nil, nil
	}
	_, err := db.Exec(`INSERT INTO jev_evaluations
		(ticket_id, model, product_decision_required, requirements_ambiguous, scope_risk,
		 policy_decision, latency_ms, input_tokens, output_tokens, mode, http_status, status, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ticketID, e.Model, product, ambiguity, scope, e.PolicyDecision, e.Latency.Milliseconds(),
		e.InputTokens, e.OutputTokens, e.Mode, e.HTTPStatus, status, errText)
	return err
}

func FormatJevEvaluation(e JevEvaluation) string {
	if e.Err != nil {
		return fmt.Sprintf("Jev evaluation failed (%dms, mode: %s): %v\nPolicy: fail open", e.Latency.Milliseconds(), e.Mode, e.Err)
	}
	policy := e.PolicyDecision
	if policy == "would_require_human_input" {
		policy = "would require human input"
	}
	return fmt.Sprintf("Jev evaluation (%dms, %s):\n  product_decision_required  %.2f\n  requirements_ambiguous     %.2f\n  scope_risk                 %.2f\n\nTokens: %d in / %d out\nPolicy: %s\nMode: %s",
		e.Latency.Milliseconds(), e.Model, e.Judgments.ProductDecisionRequired,
		e.Judgments.RequirementsAmbiguous, e.Judgments.ScopeRisk, e.InputTokens,
		e.OutputTokens, policy, e.Mode)
}
