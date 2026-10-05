// metatarget points the MetaPrinter CUPS queue at the configured physical
// printer. It runs as root from a systemd oneshot unit so that end users never
// need lpadmin rights; changing the target means editing the config and
// restarting the unit.
//
// Usage:
//
//	metatarget [-config /etc/meta-printer/target.json]
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/AntonSkrub/meta-printer/pkg/config"
)

// runCmd is replaceable in tests.
var runCmd = func(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput() // #nosec G204 -- args are validated by config.Target
	return string(out), err
}

func main() {
	path := flag.String("config", config.DefaultTargetPath, "path to the target printer config")
	flag.Parse()

	if err := apply(*path); err != nil {
		fmt.Fprintf(os.Stderr, "metatarget: %v\n", err)
		os.Exit(1)
	}
}

func apply(path string) error {
	t, err := config.LoadTarget(path)
	if err != nil {
		return err
	}

	uri := t.URI
	if t.Queue != "" {
		if uri, err = deviceURI(t.Queue); err != nil {
			return fmt.Errorf("resolve queue %q: %w", t.Queue, err)
		}
	}

	current, err := deviceURI(t.MetaQueue)
	if err != nil {
		return fmt.Errorf("read current target of %q: %w", t.MetaQueue, err)
	}
	if current == uri {
		fmt.Printf("metatarget: %s already points at %s\n", t.MetaQueue, uri)
		return nil
	}

	if out, err := runCmd("lpadmin", "-p", t.MetaQueue, "-v", uri); err != nil {
		return fmt.Errorf("lpadmin: %w: %s", err, strings.TrimSpace(out))
	}
	fmt.Printf("metatarget: %s now points at %s\n", t.MetaQueue, uri)
	return nil
}

// deviceURI returns the device URI of an existing CUPS queue.
func deviceURI(queue string) (string, error) {
	out, err := runCmd("lpstat", "-v", queue)
	if err != nil {
		return "", fmt.Errorf("lpstat: %w: %s", err, strings.TrimSpace(out))
	}
	return parseDeviceURI(out)
}

// parseDeviceURI extracts the URI from `lpstat -v` output such as
// "device for HP_LaserJet: ipp://printer.local/ipp/print".
func parseDeviceURI(out string) (string, error) {
	for _, line := range strings.Split(out, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "device for ")
		if !ok {
			continue
		}
		if _, uri, found := strings.Cut(rest, ": "); found && uri != "" {
			return strings.TrimSpace(uri), nil
		}
	}
	return "", fmt.Errorf("no device URI in output %q", strings.TrimSpace(out))
}
