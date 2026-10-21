package store

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"strings"
)

// schemaSQL is the schema the PHP panel creates on its first start. It is the
// output of "php symfony doctrine:build --all-classes --sql" in eBot-CSGO-Web,
// unchanged: one statement per line, CREATE TABLE first and the foreign keys
// after. The bot and the PHP panel read and write these tables, so the
// definitions must stay identical to what Doctrine generates. Regenerate the
// file when schema.yml changes, never edit it by hand.
//
//go:embed schema.sql
var schemaSQL string

type statement struct {
	sql   string
	table string
	// create is true for CREATE TABLE and false for ALTER TABLE.
	create bool
}

func parseSchema(src string) []statement {
	var out []statement
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "CREATE TABLE "):
			out = append(out, statement{sql: line, table: firstWord(line[len("CREATE TABLE "):]), create: true})
		case strings.HasPrefix(line, "ALTER TABLE "):
			out = append(out, statement{sql: line, table: firstWord(line[len("ALTER TABLE "):])})
		}
	}
	return out
}

func firstWord(s string) string {
	if i := strings.IndexAny(s, " ("); i >= 0 {
		s = s[:i]
	}
	return s
}

// SchemaTables lists the tables the schema defines.
func SchemaTables() []string {
	var names []string
	for _, st := range parseSchema(schemaSQL) {
		if st.create {
			names = append(names, st.table)
		}
	}
	return names
}

// MigrateResult says what Migrate did.
type MigrateResult struct {
	// Created lists the tables that were missing and have been created.
	Created []string
}

// Migrate creates the tables of the eBot schema that do not exist yet, with
// their foreign keys. It never alters, drops or changes a table that exists,
// so it is safe on a database the PHP panel created. On an empty database it
// builds everything, which makes the PHP panel unnecessary.
//
// If a statement fails, the tables created by this call are dropped again, so
// the next start sees a clean state instead of a half built schema.
func (s *Store) Migrate(ctx context.Context) (MigrateResult, error) {
	return s.migrate(ctx, parseSchema(schemaSQL))
}

func (s *Store) migrate(ctx context.Context, stmts []statement) (MigrateResult, error) {
	var res MigrateResult

	// One connection for the whole run: the lock and the session setting below
	// belong to it.
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return res, err
	}
	defer conn.Close()

	// Serialise instances that start at the same time.
	var got int
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK('ebot_web_migrate', 60)").Scan(&got); err != nil {
		return res, fmt.Errorf("migrate: lock: %w", err)
	}
	if got != 1 {
		return res, fmt.Errorf("migrate: another instance holds the lock")
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), "SELECT RELEASE_LOCK('ebot_web_migrate')") }()

	rows, err := conn.QueryContext(ctx, "SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE()")
	if err != nil {
		return res, fmt.Errorf("migrate: list tables: %w", err)
	}
	existing := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return res, err
		}
		existing[strings.ToLower(name)] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}

	missing := map[string]bool{}
	for _, st := range stmts {
		if st.create && !existing[strings.ToLower(st.table)] {
			missing[st.table] = true
		}
	}
	if len(missing) == 0 {
		return res, nil
	}

	fail := func(st statement, err error) (MigrateResult, error) {
		// Put the database back the way it was found.
		_, _ = conn.ExecContext(context.Background(), "SET FOREIGN_KEY_CHECKS = 0")
		for i := len(res.Created) - 1; i >= 0; i-- {
			if _, derr := conn.ExecContext(context.Background(), "DROP TABLE IF EXISTS `"+res.Created[i]+"`"); derr != nil {
				slog.Error("migrate: could not remove a half created table", "table", res.Created[i], "err", derr)
			}
		}
		_, _ = conn.ExecContext(context.Background(), "SET FOREIGN_KEY_CHECKS = 1")
		created := res.Created
		res.Created = nil
		return res, fmt.Errorf("migrate: %s (after creating %d tables, now removed): %w", abbreviate(st.sql), len(created), err)
	}

	for _, st := range stmts {
		if !st.create || !missing[st.table] {
			continue
		}
		if _, err := conn.ExecContext(ctx, st.sql); err != nil {
			return fail(st, err)
		}
		res.Created = append(res.Created, st.table)
	}
	// Foreign keys of the new tables. A key on a table that already existed is
	// left alone, as the table is not ours to change.
	for _, st := range stmts {
		if st.create || !missing[st.table] {
			continue
		}
		if _, err := conn.ExecContext(ctx, st.sql); err != nil {
			return fail(st, err)
		}
	}
	return res, nil
}

func abbreviate(sql string) string {
	if len(sql) > 80 {
		return sql[:80] + "..."
	}
	return sql
}
