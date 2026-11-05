//nolint:testpackage // Tests replace the unexported runCmd and call apply directly.
package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDeviceURI(t *testing.T) {
	t.Parallel()

	got, err := parseDeviceURI("device for HP_LaserJet: ipp://printer.local/ipp/print\n")
	if err != nil || got != "ipp://printer.local/ipp/print" {
		t.Fatalf("got (%q, %v)", got, err)
	}
	if _, err := parseDeviceURI("lpstat: Invalid destination name\n"); err == nil {
		t.Error("expected error for output without a device line")
	}
}

// Not parallel: replaces the package-level runCmd.
func TestApply_RetargetsOnlyWhenURIDiffers(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(cfg, []byte(`{"queue":"HP"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	orig := runCmd
	t.Cleanup(func() { runCmd = orig })

	var lpadminCalls []string
	metaURI := "socket://127.0.0.1:9"
	runCmd = func(name string, args ...string) (string, error) {
		switch name {
		case "lpstat":
			if args[1] == "HP" {
				return "device for HP: ipp://hp.local/ipp/print\n", nil
			}
			return "device for MetaPrinter: " + metaURI + "\n", nil
		case "lpadmin":
			lpadminCalls = append(lpadminCalls, strings.Join(args, " "))
			metaURI = args[3]
			return "", nil
		}
		return "", errors.New("unexpected command " + name)
	}

	if err := apply(cfg); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(lpadminCalls) != 1 || lpadminCalls[0] != "-p MetaPrinter -v ipp://hp.local/ipp/print" {
		t.Fatalf("unexpected lpadmin calls: %v", lpadminCalls)
	}

	if err := apply(cfg); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if len(lpadminCalls) != 1 {
		t.Errorf("lpadmin should not run when URI is unchanged: %v", lpadminCalls)
	}
}
