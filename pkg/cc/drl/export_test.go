package drl

// FallbackName returns the name of the DRL controller's fallback algorithm.
// Exported for testing only.
func FallbackName(ctrl interface{}) string {
	d, ok := ctrl.(*DRLController)
	if !ok {
		return ""
	}
	return d.fallback.Name()
}
