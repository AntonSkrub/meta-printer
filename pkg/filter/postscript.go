package filter

import (
	"fmt"
	"io"
	"strings"
)

// prependPostScript prepends a PostScript cover page containing metadata to
// the PostScript document read from r, and writes the combined result to w.
//
// The cover page is a minimal valid PostScript program that renders the
// metadata text and then ejects the page before the original document begins.
func prependPostScript(m *Metadata, r io.Reader, w io.Writer) error {
	cover := buildPSCoverPage(m)
	if _, err := fmt.Fprint(w, cover); err != nil {
		return fmt.Errorf("postscript: write cover: %w", err)
	}
	if _, err := io.Copy(w, r); err != nil {
		return fmt.Errorf("postscript: copy original: %w", err)
	}
	return nil
}

// buildPSCoverPage returns a PostScript program that renders one cover page
// with the document metadata.
func buildPSCoverPage(m *Metadata) string {
	var sb strings.Builder
	lines := headerLines(m)
	sb.WriteString("%!PS-Adobe-3.0\n")
	sb.WriteString("%%Pages: (atend)\n")
	sb.WriteString("%%EndComments\n")
	sb.WriteString("%%Page: cover 1\n")
	sb.WriteString("/Helvetica findfont 14 scalefont setfont\n")
	y := 720
	for _, l := range lines {
		if l == "" {
			y -= 10
			continue
		}
		fmt.Fprintf(&sb, "50 %d moveto (%s) show\n", y, psEscapeString(l))
		y -= 20
	}
	sb.WriteString("showpage\n")
	sb.WriteString("%%EndPage\n")
	return sb.String()
}

// psEscapeString escapes special characters in a PostScript string literal.
func psEscapeString(s string) string {
	var out []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '(':
			out = append(out, '\\', '(')
		case ')':
			out = append(out, '\\', ')')
		case '\\':
			out = append(out, '\\', '\\')
		default:
			out = append(out, c)
		}
	}
	return string(out)
}
