package filter

import (
	"fmt"
	"io"
	"strings"
)

// prependText prepends a plain-text metadata header to r and writes the
// combined output to w.
func prependText(m *Metadata, r io.Reader, w io.Writer) error {
	for _, l := range headerLines(m) {
		if _, err := fmt.Fprintln(w, l); err != nil {
			return fmt.Errorf("text: write header: %w", err)
		}
	}
	// Blank separator line between header and document body.
	if _, err := fmt.Fprintln(w, strings.Repeat("-", 40)); err != nil {
		return fmt.Errorf("text: write separator: %w", err)
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return fmt.Errorf("text: write blank line: %w", err)
	}
	if _, err := io.Copy(w, r); err != nil {
		return fmt.Errorf("text: copy original: %w", err)
	}
	return nil
}
