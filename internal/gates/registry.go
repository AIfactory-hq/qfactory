// Package gates provides the gate execution framework for quality gates.
package gates

import (
	"fmt"
	"sync"

	"github.com/AIfactory-hq/qfactory/pkg/contracts"
)

// GateKey uniquely identifies a gate by level and name.
type GateKey struct {
	Level string
	Name  string
}

// String returns a string representation of the gate key.
func (k GateKey) String() string {
	return fmt.Sprintf("%s/%s", k.Level, k.Name)
}

// Registry manages available gate implementations.
type Registry struct {
	mu    sync.RWMutex
	gates map[GateKey]Gate
}

// NewRegistry creates a new empty gate registry.
func NewRegistry() *Registry {
	return &Registry{
		gates: make(map[GateKey]Gate),
	}
}

// DefaultRegistry creates a registry with all built-in gates registered.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	r.RegisterBuiltins()
	return r
}

// RegisterBuiltins registers all built-in gate implementations.
func (r *Registry) RegisterBuiltins() {
	// PR1: unit_tests
	r.Register(NewUnitTestGate())

	// PR2: integration_smoke
	r.Register(NewIntegrationSmokeGate())

	// PR3: security_scan
	r.Register(NewSecurityScanGate())
}

// Register adds a gate to the registry.
// If a gate with the same level and name already exists, it is replaced.
func (r *Registry) Register(gate Gate) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := GateKey{
		Level: gate.Level(),
		Name:  contracts.NormalizeGateName(gate.Name()),
	}
	r.gates[key] = gate
}

// Get retrieves a gate by level and name.
// Returns nil if not found.
func (r *Registry) Get(level, name string) Gate {
	r.mu.RLock()
	defer r.mu.RUnlock()

	key := GateKey{
		Level: level,
		Name:  contracts.NormalizeGateName(name),
	}
	return r.gates[key]
}

// Lookup retrieves a gate by level and name.
// Returns the gate and a boolean indicating if it was found.
func (r *Registry) Lookup(level, name string) (Gate, bool) {
	gate := r.Get(level, name)
	return gate, gate != nil
}

// List returns all registered gates.
func (r *Registry) List() []Gate {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Gate, 0, len(r.gates))
	for _, gate := range r.gates {
		result = append(result, gate)
	}
	return result
}

// Keys returns all registered gate keys.
func (r *Registry) Keys() []GateKey {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]GateKey, 0, len(r.gates))
	for key := range r.gates {
		result = append(result, key)
	}
	return result
}

// Unregister removes a gate from the registry.
func (r *Registry) Unregister(level, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := GateKey{
		Level: level,
		Name:  contracts.NormalizeGateName(name),
	}
	delete(r.gates, key)
}
