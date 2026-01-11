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
