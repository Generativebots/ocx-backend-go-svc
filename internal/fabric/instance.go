// Package fabric implements the O(n) Hub-and-Spoke architecture for AOCS.
package fabric

import "sync"

var (
	globalHub *Hub
	hubOnce   sync.Once
)

// GetHub returns the singleton Hub instance for the OCX system.
// This ensures all components share the same hub for O(n) routing.
func GetHub() *Hub {
	hubOnce.Do(func() {
		globalHub = NewHub("ocx-primary", "default", "production")
	})
	return globalHub
}

// ResetHub resets the global hub (for testing only)
func ResetHub() {
	hubOnce = sync.Once{}
	globalHub = nil
}
