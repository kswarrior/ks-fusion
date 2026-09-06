package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	sqliteIDMu sync.Mutex
	sqliteDBs  = map[int]*sqliteDB{}
	sqliteNext int
)

var (
	cancelMu   sync.Mutex
	cancelChs  = map[int]chan struct{}{}
	cancelNext int
)

// v2.4 sqlite-compatible subset (file-backed, no cgo):
// sqlite_open(path) -> id, sqlite_exec(id, sql), sqlite_query(id, sql) -> array,
// sqlite_close(id). Supports CREATE TABLE IF NOT EXISTS, DROP TABLE,
// INSERT INTO, SELECT * FROM ... [JOIN ... ON ...] [WHERE ...] [GROUP BY ...]
// [ORDER BY ...] [LIMIT ...] [OFFSET ...], DELETE FROM, UPDATE ... SET ....
// postgres_open/exec/query/close (v2.5) are the same embedded engine under a
// postgres-compatible name (no server required).

var sqliteMu = struct {
	m map[int]*sqliteDB
	n int
}{m: map[int]*sqliteDB{}}

type sqliteDB struct {
	path   string
	tables map[string][]map[string]Value
}

func sqliteLoad(path string) *sqliteDB {
	db := &sqliteDB{path: path, tables: map[string][]map[string]Value{}}
	data, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return db
	}
	var raw map[string][]map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return db
	}
	for t, rows := range raw {
		for _, r := range rows {
			m := map[string]Value{}
			for k, v := range r {
				m[k] = jsonToValue(v)
			}
			db.tables[t] = append(db.tables[t], m)
		}
	}
	return db
}

func (d *sqliteDB) save() error {
	raw := map[string][]map[string]any{}
	for t, rows := range d.tables {
		for _, r := range rows {
			m := map[string]any{}
			for k, v := range r {
				j, err := valueToJSON(v)
				if err != nil {
					return err
				}
				m[k] = j
			}
			raw[t] = append(raw[t], m)
		}
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(d.path, data, 0o644)
}

func extraBuiltinsV24() []*BuiltinObj {
	return []*BuiltinObj{
		{Name: "sqlite_open", Fn: bSqliteOpen},
		{Name: "sqlite_exec", Fn: bSqliteExec},
		{Name: "sqlite_query", Fn: bSqliteQuery},
		{Name: "sqlite_close", Fn: bSqliteClose},
		// postgres-compatible dialect over the same embedded engine (v2.5):
		// same SQL subset (CREATE/DROP/INSERT/SELECT/DELETE/UPDATE + JOIN/
		// WHERE/GROUP BY/ORDER BY/LIMIT/OFFSET), file-backed, no server.
		{Name: "postgres_open", Fn: bSqliteOpen},
		{Name: "postgres_exec", Fn: bSqliteExec},
		{Name: "postgres_query", Fn: bSqliteQuery},
		{Name: "postgres_close", Fn: bSqliteClose},
		{Name: "with_cancel", Fn: bWithCancel},
		{Name: "make_cancel", Fn: bMakeCancel},
		{Name: "cancel", Fn: bCancel},
		{Name: "is_cancelled", Fn: bIsCancelled},
	}
}

func sqliteLookup(id int) *sqliteDB {
	sqliteIDMu.Lock()
	defer sqliteIDMu.Unlock()
	return sqliteDBs[id]
}

func bSqliteOpen(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("sqlite_open", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("sqlite_open wants path string")
	}
	db := sqliteLoad(args[0].Str)
	// hold global lock briefly for id alloc
	id := 0
	// reuse sqliteMu-like via globalStateMu? use separate
	sqliteIDMu.Lock()
	sqliteNext++
	id = sqliteNext
	sqliteDBs[id] = db
	sqliteIDMu.Unlock()
	return IntV(id), nil
}

func bSqliteExec(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("sqlite_exec", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VInt || args[1].Kind != VString {
		return Nil(), fmt.Errorf("sqlite_exec wants (id, sql)")
	}
	db := sqliteLookup(args[0].Int)
	if db == nil {
		return Nil(), fmt.Errorf("sqlite_exec: bad id %d", args[0].Int)
	}
	n, err := sqliteExecStmt(db, args[1].Str)
	if err != nil {
		return Nil(), err
	}
	if err := db.save(); err != nil {
		return Nil(), err
	}
	return IntV(n), nil
}

func bSqliteQuery(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("sqlite_query", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VInt || args[1].Kind != VString {
		return Nil(), fmt.Errorf("sqlite_query wants (id, sql)")
	}
	db := sqliteLookup(args[0].Int)
	if db == nil {
		return Nil(), fmt.Errorf("sqlite_query: bad id %d", args[0].Int)
	}
	rows, err := sqliteSelect(db, args[1].Str)
	if err != nil {
		return Nil(), err
	}
	out := make([]Value, 0, len(rows))
	for _, r := range rows {
		m := map[string]Value{}
		for k, v := range r {
			m[k] = v
		}
		out = append(out, MapV(m))
	}
	return ArrV(out), nil
}

func bSqliteClose(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("sqlite_close", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VInt {
		return Nil(), fmt.Errorf("sqlite_close wants id")
	}
	sqliteIDMu.Lock()
	db, ok := sqliteDBs[args[0].Int]
	if ok {
		delete(sqliteDBs, args[0].Int)
	}
	sqliteIDMu.Unlock()
	if !ok {
		return Nil(), fmt.Errorf("sqlite_close: bad id %d", args[0].Int)
	}
	return Nil(), db.save()
}

// --- minimal SQL subset ---

var (
	reCreate = regexp.MustCompile(`(?i)^\s*CREATE\s+TABLE\s+(IF\s+NOT\s+EXISTS\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\(.*\)\s*;?\s*$`)
	reDrop   = regexp.MustCompile(`(?i)^\s*DROP\s+TABLE\s+(IF\s+EXISTS\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*;?\s*$`)
	reInsert = regexp.MustCompile(`(?i)^\s*INSERT\s+INTO\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(([^)]+)\)\s*VALUES\s*\(([^)]+)\)\s*;?\s*$`)
	reDelete = regexp.MustCompile(`(?i)^\s*DELETE\s+FROM\s+([A-Za-z_][A-Za-z0-9_]*)(?:\s+WHERE\s+(.+?))?\s*;?\s*$`)
	reUpdate = regexp.MustCompile(`(?i)^\s*UPDATE\s+([A-Za-z_][A-Za-z0-9_]*)\s+SET\s+(.+?)(?:\s+WHERE\s+(.+?))?\s*;?\s*$`)
	reSelect = regexp.MustCompile(`(?i)^\s*SELECT\s+(.*?)\s+FROM\s+([A-Za-z_][A-Za-z0-9_]*)(?:\s+JOIN\s+([A-Za-z_][A-Za-z0-9_]*)\s+ON\s+(.+?))?(?:\s+WHERE\s+(.+?))?(?:\s+GROUP\s+BY\s+([A-Za-z_][A-Za-z0-9_]*))?(?:\s+ORDER\s+BY\s+([A-Za-z_][A-Za-z0-9_.]*)(?:\s+(ASC|DESC))?)?(?:\s+LIMIT\s+(\d+))?(?:\s+OFFSET\s+(\d+))?\s*;?\s*$`)
)

func sqliteExecStmt(db *sqliteDB, sql string) (int, error) {
	s := strings.TrimSpace(sql)
	if m := reCreate.FindStringSubmatch(s); m != nil {
		t := m[2]
		if _, ok := db.tables[t]; !ok {
			db.tables[t] = []map[string]Value{}
		}
		return 0, nil
	}
	if m := reDrop.FindStringSubmatch(s); m != nil {
		delete(db.tables, m[2])
		return 0, nil
	}
	if m := reInsert.FindStringSubmatch(s); m != nil {
		t, cols, vals := m[1], splitCSV(m[2]), splitCSV(m[3])
		if len(cols) != len(vals) {
			return 0, fmt.Errorf("INSERT cols/vals mismatch")
		}
		row := map[string]Value{}
		for i, c := range cols {
			c = strings.Trim(strings.TrimSpace(c), `"'`)
			row[c] = parseSQLVal(strings.TrimSpace(vals[i]))
		}
		db.tables[t] = append(db.tables[t], row)
		return 1, nil
	}
	if m := reDelete.FindStringSubmatch(s); m != nil {
		t, where := m[1], m[2]
		rows := db.tables[t]
		if strings.TrimSpace(where) == "" {
			n := len(rows)
			db.tables[t] = nil
			return n, nil
		}
		kept := []map[string]Value{}
		del := 0
		for _, r := range rows {
			ok, err := evalWhere(r, where)
			if err != nil {
				return 0, err
			}
			if ok {
				del++
			} else {
				kept = append(kept, r)
			}
		}
		db.tables[t] = kept
		return del, nil
	}
	if m := reUpdate.FindStringSubmatch(s); m != nil {
		t, setClause, where := m[1], m[2], m[3]
		rows := db.tables[t]
		assigns, err := parseSetClause(setClause)
		if err != nil {
			return 0, err
		}
		n := 0
		for _, r := range rows {
			if strings.TrimSpace(where) != "" {
				ok, err := evalWhere(r, where)
				if err != nil {
					return 0, err
				}
				if !ok {
					continue
				}
			}
			for k, v := range assigns {
				r[k] = v
			}
			n++
		}
		return n, nil
	}
	// SELECT via exec returns count (use query for rows)
	if reSelect.MatchString(s) {
		rows, err := sqliteSelect(db, s)
		if err != nil {
			return 0, err
		}
		return len(rows), nil
	}
	return 0, fmt.Errorf("unsupported SQL (subset: CREATE/DROP/INSERT/SELECT/DELETE/UPDATE): %q", sql)
}

// parseSetClause parses `a = 1, b = 'x'` into a column->value map.
func parseSetClause(s string) (map[string]Value, error) {
	out := map[string]Value{}
	for _, part := range splitCSV(s) {
		m := regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.+?)\s*$`).FindStringSubmatch(part)
		if m == nil {
			return nil, fmt.Errorf("bad SET clause %q (want col = val, ...)", part)
		}
		out[m[1]] = parseSQLVal(strings.TrimSpace(m[2]))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty SET clause")
	}
	return out, nil
}

func sqliteSelect(db *sqliteDB, sql string) ([]map[string]Value, error) {
	m := reSelect.FindStringSubmatch(strings.TrimSpace(sql))
	if m == nil {
		return nil, fmt.Errorf("unsupported SELECT (want SELECT <cols|*> FROM <t> [JOIN ...] [WHERE ...] [GROUP BY ...] [ORDER BY ...] [LIMIT ...]): %q", sql)
	}
	cols, table := strings.TrimSpace(m[1]), m[2]
	joinTable, joinOn := strings.TrimSpace(m[3]), strings.TrimSpace(m[4])
	where := strings.TrimSpace(m[5])
	groupBy := strings.TrimSpace(m[6])
	orderBy, orderDir := strings.TrimSpace(m[7]), strings.ToUpper(strings.TrimSpace(m[8]))
	limitStr, offsetStr := strings.TrimSpace(m[9]), strings.TrimSpace(m[10])
	// base rows (+ JOIN)
	rows := db.tables[table]
	if joinTable != "" {
		joined, err := innerJoin(rows, db.tables[joinTable], joinOn)
		if err != nil {
			return nil, err
		}
		rows = joined
	}
	var filtered []map[string]Value
	for _, r := range rows {
		if where != "" {
			ok, err := evalWhere(r, where)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
		}
		filtered = append(filtered, r)
	}
	// GROUP BY + COUNT(*) aggregate (v2.5): SELECT <groupcol>, COUNT(*) [AS c] ... GROUP BY <groupcol>
	if groupBy != "" {
		return groupCount(filtered, cols, groupBy)
	}
	var out []map[string]Value
	// COUNT(*) without GROUP BY returns one row {count: N}
	if isCountStar(cols) {
		alias := countAlias(cols)
		return []map[string]Value{{alias: IntV(len(filtered))}}, nil
	}
	for _, r := range filtered {
		if cols == "*" {
			cp := map[string]Value{}
			for k, v := range r {
				cp[k] = v
			}
			out = append(out, cp)
		} else {
			cp := map[string]Value{}
			for _, c := range splitCSV(cols) {
				c = strings.Trim(strings.TrimSpace(c), `"'`)
				// strip table prefix (t.col -> col) and AS alias
				c = unqualify(stripAlias(c))
				if v, ok := r[c]; ok {
					cp[c] = v
				} else {
					cp[c] = Nil()
				}
			}
			out = append(out, cp)
		}
	}
	if out == nil {
		out = []map[string]Value{}
	}
	if orderBy != "" {
		col := unqualify(orderBy)
		desc := orderDir == "DESC"
		sort.SliceStable(out, func(i, j int) bool {
			less := compareValues(out[i][col], out[j][col]) < 0
			if desc {
				return !less && compareValues(out[i][col], out[j][col]) != 0
			}
			return less
		})
	} else {
		sort.Slice(out, func(i, j int) bool {
			// deterministic: compare JSON
			a, _ := valueToJSON(MapV(out[i]))
			b, _ := valueToJSON(MapV(out[j]))
			aj, _ := json.Marshal(a)
			bj, _ := json.Marshal(b)
			return string(aj) < string(bj)
		})
	}
	// LIMIT / OFFSET
	if offsetStr != "" || limitStr != "" {
		off := 0
		if offsetStr != "" {
			fmt.Sscanf(offsetStr, "%d", &off)
		}
		if off < 0 {
			off = 0
		}
		if off > len(out) {
			out = []map[string]Value{}
		} else {
			out = out[off:]
		}
		if limitStr != "" {
			var lim int
			fmt.Sscanf(limitStr, "%d", &lim)
			if lim < len(out) {
				out = out[:lim]
			}
		}
	}
	return out, nil
}

// innerJoin returns the inner join of left/right on `a.col = b.col` (or
// `col = col`): merged maps, right columns win on collision except the join
// key keeps the left value. Table prefixes are stripped.
func innerJoin(left, right []map[string]Value, on string) ([]map[string]Value, error) {
	m := regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_.]*)\s*=\s*([A-Za-z_][A-Za-z0-9_.]*)\s*$`).FindStringSubmatch(on)
	if m == nil {
		return nil, fmt.Errorf("bad JOIN ON %q (want a.col = b.col)", on)
	}
	lc, rc := unqualify(m[1]), unqualify(m[2])
	var out []map[string]Value
	for _, l := range left {
		lv, ok := l[lc]
		if !ok {
			continue
		}
		for _, r := range right {
			rv, ok := r[rc]
			if !ok {
				continue
			}
			if !valuesEqual(lv, rv) {
				continue
			}
			merged := map[string]Value{}
			for k, v := range l {
				merged[k] = v
			}
			for k, v := range r {
				if _, exists := merged[k]; !exists {
					merged[k] = v
				}
			}
			out = append(out, merged)
		}
	}
	if out == nil {
		out = []map[string]Value{}
	}
	return out, nil
}

// groupCount implements SELECT <g>, COUNT(*) [AS c] ... GROUP BY <g>.
func groupCount(rows []map[string]Value, cols, groupBy string) ([]map[string]Value, error) {
	parts := splitCSV(cols)
	if len(parts) == 0 {
		return nil, fmt.Errorf("bad GROUP BY select list")
	}
	alias := "count"
	hasCount := false
	for _, p := range parts {
		if isCountStar(strings.TrimSpace(p)) {
			hasCount = true
			alias = countAlias(strings.TrimSpace(p))
		}
	}
	if !hasCount {
		return nil, fmt.Errorf("GROUP BY needs COUNT(*) in select list")
	}
	groups := map[string][]map[string]Value{}
	order := []string{}
	keyOf := func(r map[string]Value) string {
		v, ok := r[groupBy]
		if !ok {
			return "\x00nil"
		}
		j, _ := valueToJSON(v)
		b, _ := json.Marshal(j)
		return string(b)
	}
	for _, r := range rows {
		k := keyOf(r)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}
	sort.Strings(order)
	var out []map[string]Value
	for _, k := range order {
		rep := groups[k][0]
		row := map[string]Value{}
		if v, ok := rep[groupBy]; ok {
			row[groupBy] = v
		} else {
			row[groupBy] = Nil()
		}
		row[alias] = IntV(len(groups[k]))
		out = append(out, row)
	}
	return out, nil
}

func unqualify(col string) string {
	if i := strings.LastIndex(col, "."); i >= 0 {
		return col[i+1:]
	}
	return col
}

func stripAlias(expr string) string {
	// `COUNT(*) AS c` / `col AS c` -> alias; else the expr itself.
	if m := regexp.MustCompile(`(?i)^(.+?)\s+AS\s+([A-Za-z_][A-Za-z0-9_]*)\s*$`).FindStringSubmatch(expr); m != nil {
		_ = m[1]
		return m[2]
	}
	fields := strings.Fields(expr)
	if len(fields) == 2 {
		// `col c` shorthand
		return fields[1]
	}
	return expr
}

func isCountStar(expr string) bool {
	e := strings.ToUpper(stripAliasQualifier(expr))
	e = strings.ReplaceAll(e, " ", "")
	return e == "COUNT(*)"
}

func stripAliasQualifier(expr string) string {
	if m := regexp.MustCompile(`(?i)^(.+?)\s+AS\s+[A-Za-z_][A-Za-z0-9_]*\s*$`).FindStringSubmatch(expr); m != nil {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(expr)
}

func countAlias(expr string) string {
	if m := regexp.MustCompile(`(?i)^.+?\s+AS\s+([A-Za-z_][A-Za-z0-9_]*)\s*$`).FindStringSubmatch(expr); m != nil {
		return m[1]
	}
	return "count"
}

func compareValues(a, b Value) int {
	if a.Kind == VNil && b.Kind == VNil {
		return 0
	}
	if a.Kind == VNil {
		return -1
	}
	if b.Kind == VNil {
		return 1
	}
	if (a.Kind == VInt || a.Kind == VFloat) && (b.Kind == VInt || b.Kind == VFloat) {
		af, _ := asFloat(a)
		bf, _ := asFloat(b)
		switch {
		case af < bf:
			return -1
		case af > bf:
			return 1
		}
		return 0
	}
	as, bs := "", ""
	if a.Kind == VString {
		as = a.Str
	} else {
		j, _ := valueToJSON(a)
		bb, _ := json.Marshal(j)
		as = string(bb)
	}
	if b.Kind == VString {
		bs = b.Str
	} else {
		j, _ := valueToJSON(b)
		bb, _ := json.Marshal(j)
		bs = string(bb)
	}
	switch {
	case as < bs:
		return -1
	case as > bs:
		return 1
	}
	return 0
}

func splitCSV(s string) []string {
	var out []string
	cur := ""
	inQ := false
	q := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQ {
			cur += string(c)
			if c == q {
				inQ = false
			}
			continue
		}
		if c == '\'' || c == '"' {
			inQ = true
			q = c
			cur += string(c)
			continue
		}
		if c == ',' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(c)
	}
	out = append(out, cur)
	return out
}

func parseSQLVal(s string) Value {
	if len(s) >= 2 && ((s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"')) {
		return StrV(s[1 : len(s)-1])
	}
	if s == "nil" || s == "NULL" {
		return Nil()
	}
	if s == "true" {
		return BoolV(true)
	}
	if s == "false" {
		return BoolV(false)
	}
	// int?
	neg := false
	t := s
	if strings.HasPrefix(t, "-") {
		neg = true
		t = t[1:]
	}
	isInt := len(t) > 0
	for _, c := range t {
		if c < '0' || c > '9' {
			isInt = false
			break
		}
	}
	if isInt {
		n := 0
		for _, c := range t {
			n = n*10 + int(c-'0')
		}
		if neg {
			n = -n
		}
		return IntV(n)
	}
	return StrV(s)
}

func evalWhere(row map[string]Value, where string) (bool, error) {
	// subset: col = val [AND col = val ...], col != val, col > N etc for ints
	parts := regexp.MustCompile(`(?i)\s+AND\s+`).Split(where, -1)
	for _, p := range parts {
		m := regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*(=|!=|<>|>|<|>=|<=)\s*(.+?)\s*$`).FindStringSubmatch(p)
		if m == nil {
			return false, fmt.Errorf("unsupported WHERE %q (subset: col = val AND ...)", p)
		}
		col, op, raw := m[1], m[2], strings.TrimSpace(m[3])
		v, ok := row[col]
		if !ok {
			v = Nil()
		}
		want := parseSQLVal(raw)
		eq := valuesEqual(v, want)
		switch op {
		case "=", "==":
			if !eq {
				return false, nil
			}
		case "!=", "<>":
			if eq {
				return false, nil
			}
		case ">", "<", ">=", "<=":
			a, aok := asFloat(v)
			b, bok := asFloat(want)
			if !aok || !bok {
				return false, nil
			}
			switch op {
			case ">":
				if !(a > b) {
					return false, nil
				}
			case "<":
				if !(a < b) {
					return false, nil
				}
			case ">=":
				if !(a >= b) {
					return false, nil
				}
			case "<=":
				if !(a <= b) {
					return false, nil
				}
			}
		}
	}
	return true, nil
}

func valuesEqual(a, b Value) bool {
	if a.Kind != b.Kind {
		if (a.Kind == VInt || a.Kind == VFloat) && (b.Kind == VInt || b.Kind == VFloat) {
			af, _ := asFloat(a)
			bf, _ := asFloat(b)
			return af == bf
		}
		return false
	}
	switch a.Kind {
	case VNil:
		return true
	case VBool:
		return a.Bool == b.Bool
	case VInt:
		return a.Int == b.Int
	case VFloat:
		return a.Float == b.Float
	case VString:
		return a.Str == b.Str
	}
	return false
}

func asFloat(v Value) (float64, bool) {
	switch v.Kind {
	case VInt:
		return float64(v.Int), true
	case VFloat:
		return v.Float, true
	}
	return 0, false
}

func bWithCancel(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("with_cancel", args, 2, 2); err != nil {
		return Nil(), err
	}
	ms, err := toMillis(args[0])
	if err != nil {
		return Nil(), fmt.Errorf("with_cancel ms: %v", err)
	}
	if args[1].Kind != VFunc && args[1].Kind != VBuiltin {
		return Nil(), fmt.Errorf("with_cancel wants func, got %s", TypeName(args[1]))
	}
	cancelMu.Lock()
	cancelNext++
	id := cancelNext
	ch := make(chan struct{})
	cancelChs[id] = ch
	cancelMu.Unlock()
	defer func() {
		cancelMu.Lock()
		delete(cancelChs, id)
		cancelMu.Unlock()
	}()
	done := make(chan Value, 1)
	errCh := make(chan error, 1)
	go func() {
		v, err := in.callValue(args[1], []Value{IntV(id)})
		if err != nil {
			errCh <- err
			return
		}
		done <- v
	}()
	select {
	case v := <-done:
		return v, nil
	case err := <-errCh:
		return Nil(), err
	case <-ch:
		return Nil(), fmt.Errorf("with_cancel: cancelled")
	case <-time.After(time.Duration(ms) * time.Millisecond):
		return Nil(), fmt.Errorf("with_cancel: timed out after %dms", ms)
	}
}

func bMakeCancel(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("make_cancel", args, 0, 0); err != nil {
		return Nil(), err
	}
	cancelMu.Lock()
	cancelNext++
	id := cancelNext
	cancelChs[id] = make(chan struct{})
	cancelMu.Unlock()
	return IntV(id), nil
}

func bIsCancelled(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("is_cancelled", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VInt {
		return Nil(), fmt.Errorf("is_cancelled wants id")
	}
	cancelMu.Lock()
	ch, ok := cancelChs[args[0].Int]
	cancelMu.Unlock()
	if !ok {
		return BoolV(true), nil
	}
	select {
	case <-ch:
		return BoolV(true), nil
	default:
		return BoolV(false), nil
	}
}

// cancelBuiltin closes cancel chan (exposed as builtin `cancel`? use close-like via with_cancel only)
// Provide `cancel(id)` as alias through existing close? Add explicit:
func init() {
	// register cancel closer lazily via extraBuiltinsV24 append below
}

func bCancel(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("cancel", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VInt {
		return Nil(), fmt.Errorf("cancel wants id")
	}
	cancelMu.Lock()
	ch, ok := cancelChs[args[0].Int]
	cancelMu.Unlock()
	if !ok {
		return Nil(), fmt.Errorf("cancel: bad id %d", args[0].Int)
	}
	select {
	case <-ch:
	default:
		close(ch)
	}
	return Nil(), nil
}
