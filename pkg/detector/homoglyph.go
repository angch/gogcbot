package detector

import (
	"strings"
)

// homoglyphMap folds visually-confusable characters into a single canonical form
// before keyword matching. Numeric homoglyphs (o/O/〇/零 -> 0, l/L -> 1) are the
// ones exploited in farm profile names; other Latin text is folded via
// lowercasing in NormalizeProfileName.
var homoglyphMap = map[rune]rune{
	'o': '0', 'O': '0', '〇': '0', '零': '0', '0': '0',
	'l': '1', 'L': '1', '1': '1',
}

// NormalizeProfileName returns name lowercased with homoglyph letters folded
// into their canonical digits. This is the form used for keyword matching.
func NormalizeProfileName(name string) string {
	return FoldHomoglyphs(strings.ToLower(name))
}

// FoldHomoglyphs replaces known visual homoglyphs (such as o/O/〇/零 to 0, and l/L to 1) with their canonical characters.
func FoldHomoglyphs(s string) string {
	var b strings.Builder
	for _, r := range s {
		if n, ok := homoglyphMap[r]; ok {
			b.WriteRune(n)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
