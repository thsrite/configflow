package metrics

import (
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// lowerTrafficDomain uses current Unicode lowercase and contextual final sigma.
// Each operation has its own stateful Caser, so concurrent reports are isolated.
func lowerTrafficDomain(value string) string { return cases.Lower(language.Und).String(value) }
