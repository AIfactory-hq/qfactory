package gates

import (
	"time"

	"github.com/AIfactory-hq/qfactory/pkg/contracts"
)

// PolicyEvaluator evaluates gate policies against run results.
type PolicyEvaluator struct{}

// NewPolicyEvaluator creates a new policy evaluator.
func NewPolicyEvaluator() *PolicyEvaluator {
	return &PolicyEvaluator{}
}

// Evaluate checks if the gate results satisfy the policy.
func (e *PolicyEvaluator) Evaluate(policy *contracts.GatePolicy, gates []contracts.GateResult) contracts.PolicyDecision {
	return e.EvaluateWithHistory(policy, gates, nil)
}

// EvaluateWithHistory checks if the gate results satisfy the policy.
// When RequireAllPassed is set, it checks the entire gate history to ensure no failures.
func (e *PolicyEvaluator) EvaluateWithHistory(policy *contracts.GatePolicy, gates []contracts.GateResult, history []contracts.GateHistoryItem) contracts.PolicyDecision {
	if policy == nil {
		// No policy means all gates are allowed
		return contracts.PolicyDecision{
			Allowed: true,
			Message: "no policy defined",
		}
	}

	decision := contracts.PolicyDecision{
		Allowed: true,
	}

	now := time.Now().UTC()
	gateMap := buildGateMap(gates)

	// Check required levels
	for _, level := range policy.RequiredLevels {
		gatesForLevel := gateMap[level]
		if len(gatesForLevel) == 0 {
			// No gates at all for this level
			decision.Missing = append(decision.Missing, contracts.GateRef{Level: level, Name: "*"})
			decision.Allowed = false
		} else {
			// Check each gate at this level
			for name, result := range gatesForLevel {
				ref := contracts.GateRef{Level: level, Name: name}
				if !result.Passed {
					decision.Failing = append(decision.Failing, ref)
					decision.Allowed = false
				} else if policy.MaxAgeSeconds > 0 {
					age := now.Sub(result.Timestamp).Seconds()
					if age > float64(policy.MaxAgeSeconds) {
						decision.Stale = append(decision.Stale, ref)
						decision.Allowed = false
					}
				}
			}
		}
	}

	// Check specific required gates
	for _, gateRef := range policy.RequiredGates {
		result, found := findGate(gateMap, gateRef.Level, gateRef.Name)
		if !found {
			decision.Missing = append(decision.Missing, gateRef)
			decision.Allowed = false
			continue
		}

		if !result.Passed {
			decision.Failing = append(decision.Failing, gateRef)
			decision.Allowed = false
		} else if policy.MaxAgeSeconds > 0 {
			age := now.Sub(result.Timestamp).Seconds()
			if age > float64(policy.MaxAgeSeconds) {
				decision.Stale = append(decision.Stale, gateRef)
				decision.Allowed = false
			}
		}
	}

	// v1.0: RequireAllPassed - check entire gate history for any failures
	if policy.RequireAllPassed && len(history) > 0 {
		failedGates := findHistoricalFailures(history, policy)
		for _, ref := range failedGates {
			// Add to failing list if not already there
			if !containsRef(decision.Failing, ref) {
				decision.Failing = append(decision.Failing, ref)
			}
		}
		if len(failedGates) > 0 {
			decision.Allowed = false
		}
	}

	// Apply fail_open semantics
	if !decision.Allowed && policy.FailOpen {
		decision.Allowed = true
		decision.Message = "fail_open enabled; issues ignored"
	} else if !decision.Allowed {
		decision.Message = buildDecisionMessage(decision)
	} else {
		decision.Message = "all required gates passed"
	}

	return decision
}

// findHistoricalFailures finds any gate failures in history for required gates.
func findHistoricalFailures(history []contracts.GateHistoryItem, policy *contracts.GatePolicy) []contracts.GateRef {
	var failures []contracts.GateRef

	for _, item := range history {
		if !item.Result.Passed {
			// Check if this gate level is required
			isRequired := false
			for _, level := range policy.RequiredLevels {
				if item.Result.Level == level {
					isRequired = true
					break
				}
			}
			// Check if this specific gate is required
			if !isRequired {
				for _, gateRef := range policy.RequiredGates {
					name := contracts.NormalizeGateName(item.Result.Name)
					if item.Result.Level == gateRef.Level && name == gateRef.Name {
						isRequired = true
						break
					}
				}
			}

			if isRequired {
				ref := contracts.GateRef{
					Level: item.Result.Level,
					Name:  contracts.NormalizeGateName(item.Result.Name),
				}
				if !containsRef(failures, ref) {
					failures = append(failures, ref)
				}
			}
		}
	}

	return failures
}

// containsRef checks if a GateRef slice contains a specific ref.
func containsRef(refs []contracts.GateRef, ref contracts.GateRef) bool {
	for _, r := range refs {
		if r.Level == ref.Level && r.Name == ref.Name {
			return true
		}
	}
	return false
}

// buildGateMap creates a map[level][name]GateResult for fast lookups.
func buildGateMap(gates []contracts.GateResult) map[string]map[string]contracts.GateResult {
	result := make(map[string]map[string]contracts.GateResult)
	for _, g := range gates {
		name := g.Name
		if name == "" {
			name = "default"
		}
		if result[g.Level] == nil {
			result[g.Level] = make(map[string]contracts.GateResult)
		}
		result[g.Level][name] = g
	}
	return result
}

// findGate looks up a gate by level and name.
func findGate(gateMap map[string]map[string]contracts.GateResult, level, name string) (contracts.GateResult, bool) {
	if name == "" {
		name = "default"
	}
	levelGates, ok := gateMap[level]
	if !ok {
		return contracts.GateResult{}, false
	}
	result, found := levelGates[name]
	return result, found
}

// buildDecisionMessage creates a human-readable message for the decision.
func buildDecisionMessage(d contracts.PolicyDecision) string {
	if len(d.Missing) > 0 {
		return "missing required gates"
	}
	if len(d.Failing) > 0 {
		return "required gates failed"
	}
	if len(d.Stale) > 0 {
		return "gate results are stale"
	}
	return "policy not satisfied"
}
