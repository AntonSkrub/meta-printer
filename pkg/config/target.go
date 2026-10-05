package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// DefaultTargetPath is the root-owned file read by metatarget at start.
const DefaultTargetPath = "/etc/meta-printer/target.json"

// DefaultMetaQueue is the CUPS queue that metafilter processes jobs for.
const DefaultMetaQueue = "MetaPrinter"

// Target selects the physical printer the MetaPrinter queue forwards to.
// Exactly one of Queue or URI must be set.
type Target struct {
	// MetaQueue is the virtual queue to retarget; defaults to DefaultMetaQueue.
	MetaQueue string `json:"metaQueue,omitempty"`
	// Queue is the CUPS queue name of the physical printer.
	Queue string `json:"queue,omitempty"`
	// URI is the device URI of the physical printer (ipp://, socket://, ...).
	URI string `json:"uri,omitempty"`
}

// LoadTarget reads and validates the target file at path.
func LoadTarget(path string) (Target, error) {
	var t Target

	data, err := os.ReadFile(path) // #nosec G304 -- path is an operator-provided flag
	if err != nil {
		return t, fmt.Errorf("read target config: %w", err)
	}

	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return t, fmt.Errorf("parse target config %s: %w", path, err)
	}

	if t.MetaQueue == "" {
		t.MetaQueue = DefaultMetaQueue
	}
	return t, t.Validate()
}

// Validate requires exactly one of Queue or URI and rejects values that could
// be mistaken for command-line options.
func (t Target) Validate() error {
	if (t.Queue == "") == (t.URI == "") {
		return errors.New("target config: set exactly one of \"queue\" or \"uri\"")
	}
	if err := validateQueueName(t.MetaQueue, "metaQueue"); err != nil {
		return err
	}
	if t.Queue != "" {
		if err := validateQueueName(t.Queue, "queue"); err != nil {
			return err
		}
		if t.Queue == t.MetaQueue {
			return errors.New("target config: \"queue\" must not be the MetaPrinter queue itself")
		}
	}
	if t.URI != "" && (!strings.Contains(t.URI, ":") || strings.HasPrefix(t.URI, "-") || strings.ContainsAny(t.URI, " \t\r\n")) {
		return fmt.Errorf("target config: invalid uri %q", t.URI)
	}
	return nil
}

func validateQueueName(name, field string) error {
	if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, " \t\r\n/#:") {
		return fmt.Errorf("target config: invalid %s %q", field, name)
	}
	return nil
}
