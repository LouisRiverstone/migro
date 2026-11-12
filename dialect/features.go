package dialect

import "strings"

// LockMode is a row-lock request for a SELECT.
type LockMode string

const (
	LockUpdate LockMode = "update" // FOR UPDATE
	LockShare  LockMode = "share"  // shared lock
)

// DatePart selects what WhereDate and friends compare.
type DatePart string

const (
	PartDate  DatePart = "date"
	PartYear  DatePart = "year"
	PartMonth DatePart = "month"
	PartDay   DatePart = "day"
)

// Features are the query-building differences between databases that the ORM needs beyond Quote.
// Every method returns "" when the database cannot do it, and the caller turns that into an
// UNSUPPORTED_FEATURE error. Identifiers arrive already quoted; Base supplies ANSI defaults, so a
// dialect overrides only what differs.
type Features interface {
	// LockSQL is the clause appended to a SELECT, e.g. "FOR UPDATE".
	LockSQL(mode LockMode) string
	// JSONExtract reads the text at a JSON path (already validated: dot-separated names or indexes).
	JSONExtract(col string, path []string) string
	// DatePart reduces a date/time column to its date, year, month or day.
	DatePart(col string, part DatePart) string
	// FullTextMatch is a boolean expression with exactly one "?" for the search text.
	// boolean asks for operator syntax (+word -word "phrase") where the database has it.
	FullTextMatch(cols []string, boolean bool) string
	// FullTextIndex is the statement that creates an index FullTextMatch can use.
	FullTextIndex(table, name string, cols []string) string
}

// FeaturesOf returns d's Features, or one that supports nothing when d predates them.
func FeaturesOf(d Dialect) Features {
	if f, ok := d.(Features); ok {
		return f
	}
	return Base{}
}

func (Base) LockSQL(mode LockMode) string {
	if mode == LockShare {
		return "FOR SHARE"
	}
	return "FOR UPDATE"
}

func (Base) JSONExtract(string, []string) string { return "" }

func (Base) DatePart(col string, part DatePart) string {
	if part == PartDate {
		return "CAST(" + col + " AS DATE)"
	}
	return "EXTRACT(" + strings.ToUpper(string(part)) + " FROM " + col + ")"
}

func (Base) FullTextMatch([]string, bool) string { return "" }

func (Base) FullTextIndex(string, string, []string) string { return "" }

// JSONPathString renders path as "$.a.b[0]" for databases that take a JSON path string.
func JSONPathString(path []string) string {
	var b strings.Builder
	b.WriteString("$")
	for _, p := range path {
		if p != "" && p[0] >= '0' && p[0] <= '9' {
			b.WriteString("[" + p + "]")
			continue
		}
		b.WriteString("." + p)
	}
	return b.String()
}
