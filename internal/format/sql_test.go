package format

import (
	"strings"
	"testing"
)

func TestFormat(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		// --- Keyword uppercasing ---
		{
			name:  "uppercase keywords",
			input: "select id, name from users where active = 1",
			want: `SELECT
    id,
    name
FROM users
WHERE active = 1`,
		},
		// --- Single column stays on SELECT line ---
		{
			name:  "single column on same line",
			input: "select count(*) from orders",
			want:  "SELECT count(*)\nFROM orders",
		},
		// --- Multi-column SELECT ---
		{
			name:  "multi-column indent",
			input: "select id, first_name, last_name, email from customers",
			want: `SELECT
    id,
    first_name,
    last_name,
    email
FROM customers`,
		},
		// --- WHERE with AND/OR ---
		{
			name:  "where with and",
			input: "select id from users where active = 1 and role = 'admin'",
			want:  "SELECT id\nFROM users\nWHERE active = 1 AND role = 'admin'",
		},
		{
			name:  "where with or",
			input: "select id from users where active = 1 or role = 'guest'",
			want:  "SELECT id\nFROM users\nWHERE active = 1 OR role = 'guest'",
		},
		// --- JOIN ---
		{
			name:  "inner join",
			input: "select e.id, d.name from employees e inner join departments d on d.id = e.dept_id",
			want: `SELECT
    e.id,
    d.name
FROM employees e
INNER JOIN departments d ON d.id = e.dept_id`,
		},
		{
			name:  "left join",
			input: "select e.id from employees e left join departments d on d.id = e.dept_id",
			want: `SELECT e.id
FROM employees e
LEFT JOIN departments d ON d.id = e.dept_id`,
		},
		// --- ORDER BY / GROUP BY ---
		{
			name:  "order by",
			input: "select id, name from users order by name",
			want: `SELECT
    id,
    name
FROM users
ORDER BY name`,
		},
		{
			name:  "group by with having",
			input: "select dept_id, count(*) from employees group by dept_id having count(*) > 5",
			want: `SELECT
    dept_id,
    count(*)
FROM employees
GROUP BY dept_id
HAVING count(*) > 5`,
		},
		// --- Idempotency ---
		{
			name:  "already formatted is unchanged",
			input: "SELECT id\nFROM users\nWHERE active = 1",
			want:  "SELECT id\nFROM users\nWHERE active = 1",
		},
		// --- Preserves string literal contents ---
		{
			name:  "string literal preserved",
			input: "select id from users where name = 'hello world from here'",
			want:  "SELECT id\nFROM users\nWHERE name = 'hello world from here'",
		},
		// --- Line comments preserved ---
		{
			name:  "line comment preserved",
			input: "select id -- the primary key\nfrom users",
			want:  "SELECT id -- the primary key\nFROM users",
		},
		// --- INSERT ---
		{
			name:  "insert into",
			input: "insert into users (name, email) values ('Alice', 'alice@example.com')",
			// No space before ( when preceded by an identifier (consistent function-call rule).
			want: "INSERT INTO users(name, email)\nVALUES ('Alice', 'alice@example.com')",
		},
		// --- UPDATE ---
		{
			name:  "update set where",
			input: "update users set name = 'Bob', active = 0 where id = 1",
			want: `UPDATE users
SET name = 'Bob',
    active = 0
WHERE id = 1`,
		},
		// --- DELETE ---
		{
			name:  "delete from where",
			input: "delete from users where id = 1",
			want:  "DELETE FROM users\nWHERE id = 1",
		},
		// --- MSSQL GO batch separator preserved ---
		{
			name:  "GO separator preserved",
			input: "select 1\ngo\nselect 2",
			want:  "SELECT 1\nGO\nSELECT 2",
		},
		// --- Blank line between multi-statement buffer ---
		{
			name:  "blank line between statements",
			input: "select 1;\nselect 2;",
			want:  "SELECT 1;\n\nSELECT 2;",
		},
		// --- Extra whitespace collapsed ---
		{
			name:  "extra whitespace collapsed",
			input: "select   id,   name   from   users",
			want: `SELECT
    id,
    name
FROM users`,
		},
		// --- UNION ---
		{
			name:  "union",
			input: "select id from a union select id from b",
			want:  "SELECT id\nFROM a\nUNION\nSELECT id\nFROM b",
		},
		{
			name:  "union all",
			input: "select id from a union all select id from b",
			want:  "SELECT id\nFROM a\nUNION ALL\nSELECT id\nFROM b",
		},
		// --- WITH (CTE) ---
		{
			name:  "with cte",
			input: "with cte as (select id from users) select * from cte",
			want:  "WITH cte AS (\n    SELECT id\n    FROM users\n)\nSELECT *\nFROM cte",
		},
		// --- Subquery in FROM ---
		{
			name:  "subquery in from",
			input: "select id from (select id, name from users where active = 1) sub",
			want:  "SELECT id\nFROM (\n    SELECT\n        id,\n        name\n    FROM users\n    WHERE active = 1\n) sub",
		},
		// --- Identifiers left as-is ---
		{
			name:  "mixed case identifiers unchanged",
			input: "select MyColumn from MyTable",
			want:  "SELECT MyColumn\nFROM MyTable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Format(tt.input)
			// Normalise line endings for comparison.
			got = strings.ReplaceAll(got, "\r\n", "\n")
			want := strings.ReplaceAll(tt.want, "\r\n", "\n")
			if got != want {
				t.Errorf("Format(%q)\ngot:\n%s\n\nwant:\n%s", tt.input, got, want)
			}
		})
	}
}

func TestFormatIdempotent(t *testing.T) {
	inputs := []string{
		"select id, name from users where active = 1 and role = 'admin' order by name",
		"select e.id, d.name from employees e inner join departments d on d.id = e.dept_id where e.active = 1",
		"insert into users (name, email) values ('Alice', 'alice@example.com')",
		"update users set name = 'Bob' where id = 1",
		"with cte as (select id from users) select * from cte",
		"select id from (select id, name from users where active = 1) sub",
	}
	for _, input := range inputs {
		first := Format(input)
		second := Format(first)
		if first != second {
			t.Errorf("Format not idempotent for %q\nfirst:\n%s\nsecond:\n%s", input, first, second)
		}
	}
}
