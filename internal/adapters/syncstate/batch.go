package syncstate

import (
	"context"
	"database/sql"
	"strings"

	"github.com/go-faster/errors"
)

// maxParams bounds the parameters of one statement, under SQLite's oldest default limit.
const maxParams = 999

// batch inserts rows with multi-row INSERT statements: one call into SQLite per batch instead of per
// row, which halves the time of a full save.
type batch struct {
	tx      *sql.Tx
	insert  string // the statement up to VALUES
	columns int
	perStmt int
	args    []any
	// full is the statement of a whole batch, prepared on first use.
	full *sql.Stmt
}

func newBatch(tx *sql.Tx, insert string, columns int) *batch {
	perStmt := maxParams / columns
	return &batch{tx: tx, insert: insert, columns: columns, perStmt: perStmt, args: make([]any, 0, perStmt*columns)}
}

// add adds one row of b.columns values, inserting a full batch.
func (b *batch) add(ctx context.Context, values ...any) error {
	b.args = append(b.args, values...)
	if len(b.args) < b.perStmt*b.columns {
		return nil
	}
	if b.full == nil {
		stmt, err := b.tx.PrepareContext(ctx, b.query(b.perStmt))
		if err != nil {
			return errors.Wrap(err, "prepare batch insert")
		}
		b.full = stmt
	}
	if _, err := b.full.ExecContext(ctx, b.args...); err != nil {
		return errors.Wrap(err, "batch insert")
	}
	b.args = b.args[:0]
	return nil
}

// flush inserts the rows added since the last full batch.
func (b *batch) flush(ctx context.Context) error {
	if len(b.args) == 0 {
		return nil
	}
	if _, err := b.tx.ExecContext(ctx, b.query(len(b.args)/b.columns), b.args...); err != nil {
		return errors.Wrap(err, "batch insert")
	}
	b.args = b.args[:0]
	return nil
}

func (b *batch) close() {
	if b.full != nil {
		_ = b.full.Close()
	}
}

// query is the insert of rows rows of placeholders.
func (b *batch) query(rows int) string {
	row := "(" + strings.Repeat("?, ", b.columns-1) + "?)"
	var query strings.Builder
	query.Grow(len(b.insert) + rows*(len(row)+2))
	query.WriteString(b.insert)
	query.WriteString(" VALUES ")
	for i := range rows {
		if i > 0 {
			query.WriteString(", ")
		}
		query.WriteString(row)
	}
	return query.String()
}
