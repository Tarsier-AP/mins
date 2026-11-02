// Package escape implements ffmpeg's two-pass filtergraph escaping and the
// concat-demuxer quoting mins.sh needs for user-controlled paths.
package escape

import "strings"

// Filter makes a path safe to embed unquoted inside a filtergraph value.
// Pass 1 escapes characters special to the option-value parser, pass 2 the
// characters special to the overall filtergraph description.
func Filter(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, `:`, `\:`)

	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, `[`, `\[`)
	s = strings.ReplaceAll(s, `]`, `\]`)
	s = strings.ReplaceAll(s, `,`, `\,`)
	s = strings.ReplaceAll(s, `;`, `\;`)
	return s
}

// Concat quotes a path for a concat-demuxer "file '...'" line: a literal
// single quote becomes '\” (close, escaped quote, reopen).
func Concat(s string) string {
	return strings.ReplaceAll(s, `'`, `'\''`)
}
