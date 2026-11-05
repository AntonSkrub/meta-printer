//nolint:testpackage // Tests intentionally cover the unexported queue-name helper.
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTargetValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		target  Target
		wantErr bool
	}{
		{"queue only", Target{MetaQueue: "MetaPrinter", Queue: "HP_LaserJet"}, false},
		{"uri only", Target{MetaQueue: "MetaPrinter", URI: "ipp://printer.local/ipp/print"}, false},
		{"neither", Target{MetaQueue: "MetaPrinter"}, true},
		{"both", Target{MetaQueue: "MetaPrinter", Queue: "HP", URI: "socket://10.0.0.2:9100"}, true},
		{"queue equals meta queue", Target{MetaQueue: "MetaPrinter", Queue: "MetaPrinter"}, true},
		{"option-like queue", Target{MetaQueue: "MetaPrinter", Queue: "-E"}, true},
		{"queue with space", Target{MetaQueue: "MetaPrinter", Queue: "My Printer"}, true},
		{"option-like uri", Target{MetaQueue: "MetaPrinter", URI: "-E"}, true},
		{"uri without scheme", Target{MetaQueue: "MetaPrinter", URI: "printer"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.target.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestLoadTarget_DefaultsMetaQueue(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(path, []byte(`{"queue":"HP_LaserJet"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := LoadTarget(path)
	if err != nil {
		t.Fatalf("LoadTarget: %v", err)
	}
	if got.MetaQueue != DefaultMetaQueue || got.Queue != "HP_LaserJet" {
		t.Errorf("unexpected target %+v", got)
	}
}

func TestLoadTarget_RejectsUnknownFieldsAndMissingFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "target.json")
	if err := os.WriteFile(path, []byte(`{"queue":"HP","qeue":"typo"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTarget(path); err == nil {
		t.Error("LoadTarget should reject unknown fields")
	}
	if _, err := LoadTarget(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("LoadTarget should fail for a missing file")
	}
}
