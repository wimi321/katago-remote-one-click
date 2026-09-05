//go:build !linux && !darwin

package service

func currentStartToken(_ int) string {
	return ""
}

func processMatches(_ ProcessState) bool {
	// The service is intentionally Linux-only. Returning false keeps status and stop safe
	// if the command is inspected on another platform without guessing process identity.
	return false
}
