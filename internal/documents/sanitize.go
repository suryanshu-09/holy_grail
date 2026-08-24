package documents

import (
	"path"
	"strings"
)

const maxFilenameLen = 255

// SanitizeFilename strips directory components and unsafe characters from an
// uploaded filename so it can never alter the storage layout. The result is a
// plain base name safe to persist and display; if nothing usable remains it
// falls back to "document.pdf".
func SanitizeFilename(name string) string {
	// Normalise separators then keep only the base name, which defeats any
	// path traversal attempt embedded in the client-supplied filename.
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)

	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_', r == ' ':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}

	out := strings.TrimSpace(b.String())
	out = strings.TrimLeft(out, ".")
	out = strings.TrimSpace(out)
	if out == "" {
		return "document.pdf"
	}

	if len(out) > maxFilenameLen {
		ext := path.Ext(out)
		cut := maxFilenameLen - len(ext)
		if cut < 1 {
			cut = 1
		}
		out = out[:cut] + ext
	}
	return out
}
