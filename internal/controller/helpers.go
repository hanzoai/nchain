package controller

import (
	"github.com/hanzoai/nchain/internal/manifests"
)

// commonLabels builds the standard label set used by all controllers.
func commonLabels(name, component string, extra map[string]string) map[string]string {
	return manifests.MergeLabels(
		manifests.StandardLabels(name, component, "", ""),
		extra,
	)
}

// selectorLabels returns minimal selector labels for a controller name
// and component.
func selectorLabels(name, component string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      name,
		"app.kubernetes.io/instance":  name,
		"app.kubernetes.io/component": component,
	}
}

// derefInt32 returns the value pointed to, or the default.
func derefInt32(p *int32, def int32) int32 {
	if p != nil {
		return *p
	}
	return def
}
