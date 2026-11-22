package harness

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Registry is a concrete, process-local adapter registry. It is intentionally
// not a dynamic plugin loader: every native kind has an explicit constructor
// and unsupported kinds remain visible as unsupported.
type Registry struct {
	mu       sync.RWMutex
	adapters map[Kind]Adapter
}

// NewRegistry creates the process-local registry for adapters whose
// implementation lives in this package. The daemon app is the composition
// root for native adapter packages, which import this package's interfaces.
// Keeping that wiring outside this package avoids an import cycle and ensures
// a configured native executable is used for both probing and execution.
func NewRegistry() *Registry {
	registry := &Registry{adapters: make(map[Kind]Adapter)}
	_ = registry.Register(KindGeneric, NewGenericAdapter())
	_ = registry.Register(KindOpenCode, NewOpenCodeAdapter())
	return registry
}

// Register adds or replaces one explicit adapter constructor result.
func (registry *Registry) Register(kind Kind, adapter Adapter) error {
	if registry == nil {
		return errors.New("harness registry is nil")
	}
	if strings.TrimSpace(string(kind)) == "" {
		return errors.New("harness adapter kind must not be empty")
	}
	if adapter == nil {
		return fmt.Errorf("harness adapter %q is nil", kind)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.adapters == nil {
		registry.adapters = make(map[Kind]Adapter)
	}
	registry.adapters[kind] = adapter
	return nil
}

// Lookup returns an explicitly registered adapter.
func (registry *Registry) Lookup(kind Kind) (Adapter, error) {
	if registry == nil {
		return nil, errors.New("harness registry is nil")
	}
	registry.mu.RLock()
	adapter, ok := registry.adapters[kind]
	registry.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownAdapter, kind)
	}
	return adapter, nil
}

// Probe invokes the selected adapter's capability probe.
func (registry *Registry) Probe(ctx context.Context, kind Kind) (Capabilities, error) {
	adapter, err := registry.Lookup(kind)
	if err != nil {
		return Capabilities{}, err
	}
	capabilities, err := adapter.Probe(ctx)
	if capabilities.Kind != kind {
		kindErr := fmt.Errorf("registered harness adapter %q reported capability kind %q", kind, capabilities.Kind)
		if err != nil {
			return capabilities, errors.Join(err, kindErr)
		}
		return capabilities, kindErr
	}
	if validateErr := capabilities.Validate(); validateErr != nil {
		if err != nil {
			return capabilities, errors.Join(err, validateErr)
		}
		return capabilities, validateErr
	}
	return capabilities, err
}

// UnavailableAdapter represents a native harness that is not installed or
// whose protocol is not verified for this daemon build.
type UnavailableAdapter struct {
	kind         Kind
	probe        func(context.Context) (Capabilities, error)
	capabilities Capabilities
}

func newUnavailableAdapter(kind Kind, reason string) *UnavailableAdapter {
	capabilities := UnsupportedCapabilities(kind, reason)
	return &UnavailableAdapter{kind: kind, capabilities: capabilities}
}

// NewOpenCodeAdapter returns the explicit unsupported OpenCode V2 adapter.
// Version/help observations do not make the private transport production-ready.
func NewOpenCodeAdapter() *UnavailableAdapter {
	return newUnavailableAdapter(KindOpenCode, "OpenCode V2 2.3.0 is unsupported: require exact-binary Windows and native Linux terminal and retained-session recovery evidence before enabling native sessions; V1 is not supported")
}

// Probe returns an unsupported projection rather than silently omitting the
// unavailable native operations.
func (adapter *UnavailableAdapter) Probe(ctx context.Context) (Capabilities, error) {
	if ctx == nil {
		return Capabilities{}, errors.New("harness probe context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return Capabilities{}, err
	}
	if adapter.probe != nil {
		return adapter.probe(ctx)
	}
	return adapter.capabilities, &AvailabilityError{Kind: adapter.kind, Reason: adapter.reason()}
}

// Start fails closed. An unavailable native adapter cannot create a session.
func (adapter *UnavailableAdapter) Start(ctx context.Context, _ StartRequest, _ EventSink) (Session, error) {
	if ctx == nil {
		return nil, errors.New("harness start context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if adapter.probe != nil {
		_, err := adapter.probe(ctx)
		if err != nil {
			return nil, err
		}
	}
	return nil, &AvailabilityError{Kind: adapter.kind, Reason: adapter.reason()}
}

func (adapter *UnavailableAdapter) reason() string {
	if len(adapter.capabilities.Unsupported) == 0 {
		return "native harness unavailable"
	}
	return adapter.capabilities.Unsupported[string(CapabilityStart)]
}

// AvailabilityError distinguishes an absent/unverified native adapter from a
// rejected operation while preserving errors.Is(ErrHarnessUnavailable).
type AvailabilityError struct {
	Kind   Kind
	Reason string
}

func (err *AvailabilityError) Error() string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", err.Kind, err.Reason)
}

func (err *AvailabilityError) Unwrap() error { return ErrHarnessUnavailable }
