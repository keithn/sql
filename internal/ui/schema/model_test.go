package schema

import (
	"strings"
	"testing"

	"github.com/sqltui/sql/internal/db"
)

// ── tableAlias ────────────────────────────────────────────────────────────────

func TestTableAlias(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"Alert", "A"},
		{"AlarmQueue", "AQ"},
		{"tblHardware", "H"},
		{"tblClient", "C"},
		{"tblAlarmQueue", "AQ"},
		{"AlertDetail", "AD"},
		{"tblHardwareSoftwareVersion", "HSV"},
		{"Hardware", "H"},
		{"vwClientSummary", "CS"},
		{"t_Orders", "O"},
	}
	for _, c := range cases {
		got := tableAlias(c.name)
		if got != c.want {
			t.Errorf("tableAlias(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// ── uniqueTableAlias ──────────────────────────────────────────────────────────

func TestUniqueTableAlias_NoCollision(t *testing.T) {
	used := map[string]bool{}
	got := uniqueTableAlias("tblHardware", used)
	if got != "H" {
		t.Fatalf("want H, got %q", got)
	}
}

func TestUniqueTableAlias_SingleCollision(t *testing.T) {
	used := map[string]bool{"H": true}
	got := uniqueTableAlias("tblHardware", used)
	// First extension: "HA" (first 2 chars of "HARDWARE")
	if got != "HA" {
		t.Fatalf("want HA, got %q", got)
	}
}

func TestUniqueTableAlias_MultipleCollisions(t *testing.T) {
	used := map[string]bool{"H": true, "HA": true, "HAR": true}
	got := uniqueTableAlias("tblHardware", used)
	if got != "HARD" {
		t.Fatalf("want HARD, got %q", got)
	}
}

func TestUniqueTableAlias_FallbackToNumber(t *testing.T) {
	// Exhaust all prefix lengths for a short name.
	used := map[string]bool{"A": true, "AL": true, "ALE": true, "ALER": true, "ALERT": true}
	got := uniqueTableAlias("Alert", used)
	if got != "A2" {
		t.Fatalf("want A2, got %q", got)
	}
}

func TestUniqueTableAlias_MainAliasReserved(t *testing.T) {
	// Simulates the main table alias already in use.
	used := map[string]bool{"AQ": true}
	got := uniqueTableAlias("tblAlarmQueue", used)
	if got == "AQ" {
		t.Fatalf("should not return already-used alias AQ")
	}
	if used[got] {
		t.Fatalf("returned alias %q is already used", got)
	}
}

// ── buildSelectedSQL ──────────────────────────────────────────────────────────

func makeModel(driver string, cols []db.ColumnDef, selected []bool) Model {
	m := New()
	m.driver = driver
	m.filtered = []tableEntry{{
		schemaName: "dbo",
		tableName:  "tblClient",
		columns:    cols,
	}}
	m.cursor = 0
	m.selectedCols = selected
	return m
}

func TestBuildSelectedSQL_NoSelection_FallsBackToStar(t *testing.T) {
	m := makeModel("mssql", []db.ColumnDef{{Name: "Id"}, {Name: "Name"}}, []bool{false, false})
	sql := m.buildSelectedSQL(m.filtered[0])
	if !strings.Contains(sql, "SELECT TOP 500 *") {
		t.Fatalf("expected SELECT *, got:\n%s", sql)
	}
}

func TestBuildSelectedSQL_MSSQLSelectedColumns(t *testing.T) {
	cols := []db.ColumnDef{{Name: "lngClientID"}, {Name: "strClientName"}, {Name: "Notes"}}
	sel := []bool{true, true, false}
	m := makeModel("mssql", cols, sel)
	sql := m.buildSelectedSQL(m.filtered[0])

	if !strings.Contains(sql, "SELECT TOP 500") {
		t.Errorf("missing SELECT TOP 500:\n%s", sql)
	}
	if !strings.Contains(sql, "[lngClientID]") {
		t.Errorf("missing lngClientID:\n%s", sql)
	}
	if !strings.Contains(sql, "[strClientName]") {
		t.Errorf("missing strClientName:\n%s", sql)
	}
	if strings.Contains(sql, "[Notes]") {
		t.Errorf("unselected Notes should not appear:\n%s", sql)
	}
	// Main alias derived from tblClient → C
	if !strings.Contains(sql, "AS C") {
		t.Errorf("expected main alias C, got:\n%s", sql)
	}
	if !strings.Contains(sql, "C.[lngClientID]") {
		t.Errorf("expected qualified column C.[lngClientID]:\n%s", sql)
	}
}

func TestBuildSelectedSQL_InnerJoinForFK(t *testing.T) {
	fk := &db.ForeignKey{RefTable: "dbo.tblAlarmType", RefColumn: "lngAlarmTypeID"}
	cols := []db.ColumnDef{
		{Name: "Id"},
		{Name: "lngAlarmTypeID", ForeignKey: fk},
	}
	sel := []bool{true, true}
	m := makeModel("mssql", cols, sel)
	sql := m.buildSelectedSQL(m.filtered[0])

	if !strings.Contains(sql, "INNER JOIN") {
		t.Errorf("expected INNER JOIN, got:\n%s", sql)
	}
	if strings.Contains(sql, "LEFT JOIN") {
		t.Errorf("should not contain LEFT JOIN:\n%s", sql)
	}
	// FK alias from tblAlarmType → AT
	if !strings.Contains(sql, "AS AT") {
		t.Errorf("expected FK alias AT for tblAlarmType:\n%s", sql)
	}
	if !strings.Contains(sql, "ON C.[lngAlarmTypeID] = AT.[lngAlarmTypeID]") {
		t.Errorf("expected JOIN condition:\n%s", sql)
	}
	// Joined table should be projected as alias.*
	if !strings.Contains(sql, "AT.*") {
		t.Errorf("expected AT.* for joined table:\n%s", sql)
	}
}

func TestBuildSelectedSQL_JoinAliasDoesNotCollideWithMain(t *testing.T) {
	// Main table tblClient → C; FK ref tblCategory → C (collision) → should disambiguate.
	fk := &db.ForeignKey{RefTable: "dbo.tblCategory", RefColumn: "Id"}
	cols := []db.ColumnDef{
		{Name: "Name"},
		{Name: "CategoryId", ForeignKey: fk},
	}
	sel := []bool{true, true}
	m := makeModel("mssql", cols, sel)
	sql := m.buildSelectedSQL(m.filtered[0])

	// Both "C" (main) and join alias must be present but must differ.
	if !strings.Contains(sql, "AS C") {
		t.Fatalf("expected main alias C:\n%s", sql)
	}
	// Join alias should NOT be C again.
	// tblCategory stripped → Category → first extension is CA.
	if !strings.Contains(sql, "AS CA") {
		t.Fatalf("expected join alias CA to avoid C collision:\n%s", sql)
	}
}

func TestBuildSelectedSQL_PostgresUsesLimit(t *testing.T) {
	cols := []db.ColumnDef{{Name: "email"}}
	sel := []bool{true}
	m := New()
	m.driver = "postgres"
	m.filtered = []tableEntry{{schemaName: "public", tableName: "users", columns: cols}}
	m.cursor = 0
	m.selectedCols = sel
	sql := m.buildSelectedSQL(m.filtered[0])

	if !strings.Contains(sql, "LIMIT 500") {
		t.Errorf("expected LIMIT 500 for postgres:\n%s", sql)
	}
	if strings.Contains(sql, "TOP") {
		t.Errorf("postgres should not use TOP:\n%s", sql)
	}
}
