package protocol

import "sync"

var builtinRegistry *Registry
var builtinRegistryMu sync.Mutex

// Builtins returns the process-wide host protocol registry.
// Host-backed Go adapters have been removed; shipped protocols load from plugin packages.
func Builtins() *Registry {
	builtinRegistryMu.Lock()
	defer builtinRegistryMu.Unlock()
	if builtinRegistry != nil {
		return builtinRegistry
	}
	registry, err := NewRegistry()
	if err != nil {
		panic(err)
	}
	builtinRegistry = registry
	return builtinRegistry
}

// BundledManifest returns the canonical unified manifest for a declarative bundled contribution.
// Host manifests are no longer embedded in the application binary.
func BundledManifest(id string) (Manifest, bool) {
	_ = id
	return Manifest{}, false
}
