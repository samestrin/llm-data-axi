package utils

import (
	"testing"
)

func TestExcludedDirs(t *testing.T) {
	// These directories should be excluded
	excluded := []string{
		"node_modules",
		".git",
		"__pycache__",
		"dist",
		"build",
		"vendor",
		".venv",
		"coverage",
	}

	for _, dir := range excluded {
		if !ExcludedDirs[dir] {
			t.Errorf("ExcludedDirs should contain %q", dir)
		}
	}
}

func TestIsExcludedDir(t *testing.T) {
	tests := []struct {
		name     string
		dirName  string
		expected bool
	}{
		{"node_modules excluded", "node_modules", true},
		{".git excluded", ".git", true},
		{"src not excluded", "src", false},
		{"lib not excluded", "lib", false},
		{"vendor excluded", "vendor", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsExcludedDir(tt.dirName)
			if result != tt.expected {
				t.Errorf("IsExcludedDir(%q) = %v, want %v", tt.dirName, result, tt.expected)
			}
		})
	}
}
