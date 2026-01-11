package gates

import (
	"time"

	"github.com/AIfactory-hq/qfactory/pkg/contracts"
)

// TrustCalculator computes a trust index for a workflow run based on gate results.
type TrustCalculator struct{}

// NewTrustCalculator creates a new trust calculator.
func NewTrustCalculator() *TrustCalculator {
	return &TrustCalculator{}
}

// Calculate computes the trust index based on gate results and optional policy.
func (c *TrustCalculator) Calculate(policy *contracts.GatePolicy, gates []contracts.GateResult) contracts.TrustIndex {
	if len(gates) == 0 {
		return contracts.TrustIndex{
			Score: 0,
			Grade: "F",
			Breakdown: contracts.TrustBreakdown{
				PassedRequired: 0,
				Failed:         0,
				Stale:          0,
				Missing:        0,
			},
		}
	}

	breakdown := contracts.TrustBreakdown{}
	now := time.Now().UTC()

	// If policy is defined, evaluate against it
	if policy != nil {
		breakdown = c.evaluateWithPolicy(policy, gates, now)
	} else {
		// No policy: just count passed vs failed
		breakdown = c.evaluateWithoutPolicy(gates)
	}

	score := c.computeScore(breakdown, len(gates))
	grade := c.scoreToGrade(score)

	return contracts.TrustIndex{
		Score:     score,
		Grade:     grade,
		Breakdown: breakdown,
	}
}

// evaluateWithPolicy computes breakdown when a policy is defined.
func (c *TrustCalculator) evaluateWithPolicy(policy *contracts.GatePolicy, gates []contracts.GateResult, now time.Time) contracts.TrustBreakdown {
	breakdown := contracts.TrustBreakdown{}
	gateMap := buildGateMap(gates)

	// Track required gates we've seen
	requiredGates := make(map[string]bool)

	// Collect all required gate refs
	for _, level := range policy.RequiredLevels {
		levelGates, exists := gateMap[level]
		if !exists {
			breakdown.Missing++
			continue
		}
		for name := range levelGates {
			requiredGates[level+":"+name] = true
		}
	}
	for _, ref := range policy.RequiredGates {
		requiredGates[ref.Level+":"+ref.Name] = true
	}

	// Evaluate each gate
	for _, g := range gates {
		name := g.Name
		if name == "" {
			name = "default"
		}
		key := g.Level + ":" + name
		isRequired := requiredGates[key]

		if !g.Passed {
			breakdown.Failed++
		} else if policy.MaxAgeSeconds > 0 {
			age := now.Sub(g.Timestamp).Seconds()
			if age > float64(policy.MaxAgeSeconds) {
				breakdown.Stale++
			} else if isRequired {
				breakdown.PassedRequired++
			}
		} else if isRequired {
			breakdown.PassedRequired++
		}
	}

	// Count missing required gates
	for _, level := range policy.RequiredLevels {
		if _, exists := gateMap[level]; !exists {
			breakdown.Missing++
		}
	}
	for _, ref := range policy.RequiredGates {
		if _, found := findGate(gateMap, ref.Level, ref.Name); !found {
			breakdown.Missing++
		}
	}

	return breakdown
}

// evaluateWithoutPolicy computes breakdown when no policy is defined.
func (c *TrustCalculator) evaluateWithoutPolicy(gates []contracts.GateResult) contracts.TrustBreakdown {
	breakdown := contracts.TrustBreakdown{}

	for _, g := range gates {
		if g.Passed {
			breakdown.PassedRequired++
		} else {
			breakdown.Failed++
		}
	}

	return breakdown
}

// computeScore calculates the 0-100 trust score.
func (c *TrustCalculator) computeScore(breakdown contracts.TrustBreakdown, totalGates int) int {
	if totalGates == 0 {
		return 0
	}

	// Weight factors
	const (
		passedWeight  = 100
		failedWeight  = -50
		staleWeight   = -20
		missingWeight = -30
	)

	// Calculate base score from passed gates
	passedRatio := float64(breakdown.PassedRequired) / float64(totalGates)
	baseScore := passedRatio * float64(passedWeight)

	// Apply penalties
	penalties := float64(breakdown.Failed)*float64(-failedWeight) +
		float64(breakdown.Stale)*float64(-staleWeight) +
		float64(breakdown.Missing)*float64(-missingWeight)

	// Normalize penalties based on total gates
	if totalGates > 0 {
		penalties = penalties / float64(totalGates)
	}

	score := int(baseScore - penalties)

	// Clamp to 0-100
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return score
}

// scoreToGrade converts a numeric score to a letter grade.
func (c *TrustCalculator) scoreToGrade(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 80:
		return "B"
	case score >= 70:
		return "C"
	case score >= 60:
		return "D"
	default:
		return "F"
	}
}

// CalculateWithHistory computes trust index using gate history for advanced scoring.
// History is used to:
// - Penalize consecutive failures (indicates systemic issues)
// - Reward successful retries (indicates recovery)
// - Detect flaky gates (alternating pass/fail patterns)
func (c *TrustCalculator) CalculateWithHistory(policy *contracts.GatePolicy, gates []contracts.GateResult, history []contracts.GateHistoryItem) contracts.TrustIndex {
	// Start with base calculation
	base := c.Calculate(policy, gates)

	if len(history) == 0 {
		return base
	}

	// Analyze history for each unique gate (level+name)
	gateHistories := buildGateHistories(history)

	var consecutiveFailurePenalty int
	var flakyPenalty int
	var recoveryReward int

	for _, executions := range gateHistories {
		if len(executions) < 2 {
			continue
		}

		// Analyze patterns
		patterns := analyzeExecutionPatterns(executions)

		consecutiveFailurePenalty += patterns.consecutiveFailures * 5 // -5 per consecutive failure
		flakyPenalty += patterns.flakyCount * 3                       // -3 per flaky detection
		recoveryReward += patterns.recoveryCount * 10                 // +10 for each successful retry recovery
	}

	// Apply history-based adjustments
	adjustedScore := base.Score
	adjustedScore -= consecutiveFailurePenalty
	adjustedScore -= flakyPenalty
	adjustedScore += recoveryReward

	// Clamp to 0-100
	if adjustedScore < 0 {
		adjustedScore = 0
	}
	if adjustedScore > 100 {
		adjustedScore = 100
	}

	// Update breakdown with history-based info
	base.Breakdown.ConsecutiveFailurePenalty = consecutiveFailurePenalty
	base.Breakdown.FlakyPenalty = flakyPenalty
	base.Breakdown.RecoveryReward = recoveryReward

	base.Score = adjustedScore
	base.Grade = c.scoreToGrade(adjustedScore)

	return base
}

// gateKey returns a unique key for a gate (level:name).
func gateKey(level, name string) string {
	if name == "" {
		name = "default"
	}
	return level + ":" + name
}

// buildGateHistories groups history items by gate (level+name), ordered by timestamp.
func buildGateHistories(history []contracts.GateHistoryItem) map[string][]contracts.GateHistoryItem {
	result := make(map[string][]contracts.GateHistoryItem)

	for _, item := range history {
		key := gateKey(item.Result.Level, item.Result.Name)
		result[key] = append(result[key], item)
	}

	// Note: history is already ordered by timestamp desc from the store,
	// so newer executions come first

	return result
}

// executionPatterns holds analysis results for a gate's execution history.
type executionPatterns struct {
	consecutiveFailures int // number of consecutive failures in recent history
	flakyCount          int // number of pass/fail alternations
	recoveryCount       int // number of successful retries after failure
}

// analyzeExecutionPatterns analyzes execution patterns for a gate.
func analyzeExecutionPatterns(executions []contracts.GateHistoryItem) executionPatterns {
	if len(executions) < 2 {
		return executionPatterns{}
	}

	patterns := executionPatterns{}

	// Count consecutive failures from most recent
	for _, exec := range executions {
		if !exec.Result.Passed {
			patterns.consecutiveFailures++
		} else {
			break
		}
	}

	// Detect flakiness and recovery
	// Look for patterns in the history
	for i := 0; i < len(executions)-1; i++ {
		current := executions[i]
		previous := executions[i+1]

		// Recovery: current passes, previous failed, and is a retry
		if current.Result.Passed && !previous.Result.Passed && current.ParentExecutionID != nil {
			patterns.recoveryCount++
		}

		// Flaky: alternating pass/fail pattern
		if current.Result.Passed != previous.Result.Passed {
			// Check for alternating pattern (need at least 3 points)
			if i+2 < len(executions) {
				beforePrevious := executions[i+2]
				// Pattern: pass-fail-pass or fail-pass-fail = flaky
				if current.Result.Passed == beforePrevious.Result.Passed && current.Result.Passed != previous.Result.Passed {
					patterns.flakyCount++
				}
			}
		}
	}

	return patterns
}
