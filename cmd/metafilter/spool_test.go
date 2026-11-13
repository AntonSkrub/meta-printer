package main

import (
	"path/filepath"
	"testing"
)

func TestValidateSpoolPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"valid file", filepath.Join(dir, "d00030-001"), true},
		{"relative path", "d00030-001", false},
		{"outside spool", filepath.Join(filepath.Dir(dir), "secret"), false},
		{"traversal outside spool", dir + string(filepath.Separator) + ".." + string(filepath.Separator) + "secret", false},
		{"nested path", filepath.Join(dir, "nested", "file"), false},
		{"spool directory", dir, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateSpoolPath(tt.path, dir)
			if (err == nil) != tt.want {
				t.Fatalf("validateSpoolPath(%q) = %q, %v", tt.path, got, err)
			}
			if tt.want && got != tt.path {
				t.Fatalf("got %q, want %q", got, tt.path)
			}
		})
	}
}
