package metrics

import (
	"context"
	"database/sql"
	"strings"
)

// numericCountersSQL is used only with the two constant table names or an empty
// prefix. SQLite otherwise silently coerces a hand-edited TEXT counter to zero.
func numericCountersSQL(prefix string) string {
	clauses := make([]string, 0, 9)
	for _, field := range []string{"up", "down", "conns"} {
		name := prefix + field
		clauses = append(clauses, "typeof("+name+") IN ('integer','real')", name+" <= 1.7976931348623157e308", name+" >= -1.7976931348623157e308")
	}
	return strings.Join(clauses, " AND ")
}
func counterUpdateSQL(table string) string {
	clauses := []string{numericCountersSQL(table + ".")}
	for _, field := range []string{"up", "down", "conns"} {
		sum := "(" + table + "." + field + " + excluded." + field + ")"
		clauses = append(clauses, sum+" <= 1.7976931348623157e308", sum+" >= -1.7976931348623157e308")
	}
	return " WHERE " + strings.Join(clauses, " AND ")
}
func (c *trafficConn) upsertContext(ctx context.Context, statement string, args ...any) error {
	n, err := c.execContext(ctx, statement, args...)
	if err != nil {
		return err
	}
	if n != 1 {
		return &TypeError{Message: "invalid stored traffic counter"}
	}
	return nil
}
func validateGroupedCounters(rows *sql.Rows, row []any) error {
	pointers := make([]any, 0, len(row)+1)
	for i := range row {
		pointers = append(pointers, &row[i])
	}
	var valid int
	pointers = append(pointers, &valid)
	if err := rows.Scan(pointers...); err != nil {
		return err
	}
	if valid != 1 {
		return &TypeError{Message: "invalid stored traffic counter"}
	}
	_, err := tripleFromSQL(row[len(row)-3], row[len(row)-2], row[len(row)-1])
	return err
}
