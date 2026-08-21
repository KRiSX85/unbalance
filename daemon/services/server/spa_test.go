package server

import "testing"

func TestIsSPAClientPath(t *testing.T) {
	keep := []string{
		"/",
		"/auto-gather",
		"/auto-gather/",
		"/scatter",
		"/scatter/select",
		"/gather",
		"/gather/select",
		"/history",
		"/settings",
		"/settings/notifications",
		"/logs",
		"/log",
		"/login",
	}
	for _, path := range keep {
		if !isSPAClientPath(path) {
			t.Fatalf("isSPAClientPath(%q) = false, want true", path)
		}
	}
	reject := []string{"/api/state", "/api/auto-gather/controlled/status", "/ws", "/assets/index.js"}
	for _, path := range reject {
		if isSPAClientPath(path) {
			t.Fatalf("isSPAClientPath(%q) = true, want false", path)
		}
	}
}
