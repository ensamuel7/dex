package stdlib

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/db.c
var dbRuntime string

var bundledSqliteOptions = []string{
	"-DSQLITE_THREADSAFE=1",
	"-DSQLITE_DEFAULT_MEMSTATUS=0",
	"-DSQLITE_USE_ALLOCA",
	"-DSQLITE_ALLOW_ROWID_IN_VIEW",
	"-DSQLITE_DEFAULT_AUTOVACUUM=1",
	"-DSQLITE_DEFAULT_RECURSIVE_TRIGGERS=1",
	"-DSQLITE_DEFAULT_SECTOR_SIZE=4096",
	"-DSQLITE_DIRECT_OVERFLOW_READ",
	"-DSQLITE_ENABLE_COLUMN_METADATA",
	"-DSQLITE_ENABLE_DBPAGE_VTAB",
	"-DSQLITE_ENABLE_DBSTAT_VTAB",
	"-DSQLITE_ENABLE_FTS3",
	"-DSQLITE_ENABLE_FTS3_PARENTHESIS",
	"-DSQLITE_ENABLE_FTS3_TOKENIZER",
	"-DSQLITE_ENABLE_FTS4",
	"-DSQLITE_ENABLE_FTS5",
	"-DSQLITE_ENABLE_MATH_FUNCTIONS",
	"-DSQLITE_ENABLE_PREUPDATE_HOOK",
	"-DSQLITE_ENABLE_RTREE",
	"-DSQLITE_ENABLE_SESSION",
	"-DSQLITE_ENABLE_STMTVTAB",
	"-DSQLITE_ENABLE_UNLOCK_NOTIFY",
	"-DSQLITE_LIKE_DOESNT_MATCH_BLOBS",
	"-DSQLITE_MAX_SCHEMA_RETRY=25",
	"-DSQLITE_MAX_VARIABLE_NUMBER=250000",
	"-DSQLITE_SECURE_DELETE",
	"-DSQLITE_SOUNDEX",
	"-DSQLITE_USE_URI=1",
}

func sqliteFlags(src string) ([]string, error) {
	if src == "" {
		return []string{"-lsqlite3"}, nil
	}
	abs, err := filepath.Abs(src)
	if err != nil || filepath.Base(abs) != "sqlite3.c" {
		return nil, fmt.Errorf("DEX_SQLITE must name an SQLite amalgamation's sqlite3.c, got %q", src)
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("DEX_SQLITE: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(abs), "sqlite3.h")); err != nil {
		return nil, fmt.Errorf("DEX_SQLITE: no sqlite3.h beside %s", abs)
	}
	flags := []string{"-I" + filepath.Dir(abs)}
	flags = append(flags, bundledSqliteOptions...)
	return append(flags, abs, "-lm"), nil
}

func detectDbFlags() []string {
	flags, err := sqliteFlags(os.Getenv("DEX_SQLITE"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Check for PostgreSQL
	if out, err := exec.Command("pg_config", "--includedir").Output(); err == nil {
		incDir := strings.TrimSpace(string(out))
		flags = append(flags, "-DDEX_HAS_POSTGRES", "-I"+incDir)
		if libOut, libErr := exec.Command("pg_config", "--libdir").Output(); libErr == nil {
			libDir := strings.TrimSpace(string(libOut))
			flags = append(flags, "-L"+libDir)
		}
		flags = append(flags, "-lpq")
	}

	// Check for MySQL
	// mysql_config --include returns e.g. -I/path/to/include/mysql
	// but db.c uses #include <mysql/mysql.h>, so we need the parent directory
	if incOut, err := exec.Command("mysql_config", "--include").Output(); err == nil {
		inc := strings.TrimSpace(string(incOut))
		// Convert -I/path/include/mysql to -I/path/include
		for _, f := range strings.Fields(inc) {
			if strings.HasPrefix(f, "-I") && strings.HasSuffix(f, "/mysql") {
				f = f[:len(f)-len("/mysql")]
			}
			flags = append(flags, f)
		}
		flags = append(flags, "-DDEX_HAS_MYSQL")
		// Extract only -L flags from mysql_config --libs, add -lmysqlclient
		if libOut, libErr := exec.Command("mysql_config", "--libs").Output(); libErr == nil {
			for _, f := range strings.Fields(strings.TrimSpace(string(libOut))) {
				if strings.HasPrefix(f, "-L") {
					flags = append(flags, f)
				}
			}
		}
		flags = append(flags, "-lmysqlclient")
	}

	// Check for MongoDB (mongoc)
	if out, err := exec.Command("pkg-config", "--cflags", "libmongoc-1.0").Output(); err == nil {
		cflags := strings.TrimSpace(string(out))
		flags = append(flags, "-DDEX_HAS_MONGO")
		flags = append(flags, strings.Fields(cflags)...)
		flags = append(flags, "-lmongoc-1.0", "-lbson-1.0")
	}

	return flags
}

func init() {
	Register(&Module{
		Name: "db",
		Funcs: map[string]FuncDef{
			"open": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"driver", "dsn"},
				ReturnType: ast.TypeInt,
				CName:      "dex_db_open",
				Doc:        "Open a database connection with the given driver and DSN.",
			},
			"openSingleThreaded": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"driver", "dsn"},
				ReturnType: ast.TypeInt,
				CName:      "dex_db_open_single_threaded",
				Doc:        "Open a connection that is never used by two threads at once, such as one only http.inline() or http.uring() handlers touch. For SQLite it skips the lock SQLite otherwise takes on every call, which is a few percent of a busy server's CPU. Using it from two threads at the same time corrupts it, so a connection shared by the default handler pool must use db.open. Other drivers behave as with db.open.",
			},
			"exec": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString},
				ParamNames: []string{"conn", "query"},
				ReturnType: ast.TypeInt,
				CName:      "dex_db_exec",
				Doc:        "Execute a SQL statement that does not return rows.",
			},
			"query": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString},
				ParamNames: []string{"conn", "query"},
				ReturnType: ast.TypeInt,
				CName:      "dex_db_query",
				Doc:        "Execute a SQL query and return a result handle.",
			},
			"next": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"result"},
				ReturnType: ast.TypeBool,
				CName:      "dex_db_next",
				Doc:        "Advance to the next row in a result set. Returns false when done.",
			},
			"col": {
				Params:     nil, // polymorphic — return type resolved from context
				ParamNames: []string{"result", "index"},
				ReturnType: ast.TypeInt,
				CName:      "",
				Doc:        "Get a column value from the current row by index.",
			},
			"fill": {
				Params:     nil, // polymorphic — the subject is any struct
				ParamNames: []string{"into", "result"},
				ReturnType: ast.TypeVoid,
				CName:      "",
				Doc:        "Fill a struct from the current row, reading column i into field i. The subject must be a &Struct or a local variable, never a by-value parameter. Field order is column order, so the projection must select one column per field in declaration order — query a struct's own field list to build it. A field reflection cannot read (an array, a map, a nested struct) is skipped but still consumes its column position, so the fields after it stay aligned.",
			},
			"free": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"result"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_db_free",
				Doc:        "Free a query result handle.",
			},
			"close": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"conn"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_db_close",
				Doc:        "Close a database connection.",
			},
			"queryParams": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString, ast.TypeArrayString},
				ParamNames: []string{"conn", "sql", "values"},
				ReturnType: ast.TypeInt,
				CName:      "dex_db_query_params",
				Doc:        "Execute a query with bound parameters ($1, $2, ...) and return a result handle. Postgres only.",
			},
			"execParams": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString, ast.TypeArrayString},
				ParamNames: []string{"conn", "sql", "values"},
				ReturnType: ast.TypeInt,
				CName:      "dex_db_exec_params",
				Doc:        "Execute a statement with bound parameters ($1, $2, ...). Returns affected rows. Postgres only.",
			},
			"prepare": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString},
				ParamNames: []string{"conn", "sql"},
				ReturnType: ast.TypeInt,
				CName:      "dex_db_prepare",
				Doc:        "Prepare a SQL statement with parameter placeholders. Returns a statement handle.",
			},
			"bindInt": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeInt, ast.TypeInt},
				ParamNames: []string{"stmt", "index", "value"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_db_bind_int",
				Doc:        "Bind an integer value to a prepared statement parameter by index (1-based).",
			},
			"bindLong": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeInt, ast.TypeLong},
				ParamNames: []string{"stmt", "index", "value"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_db_bind_long",
				Doc:        "Bind a 64-bit integer to a prepared statement parameter by index (1-based). bindInt truncates past 2^31, which an id column reaches; db.col hands ids back as long for the same reason.",
			},
			"bindDouble": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeInt, ast.TypeDouble},
				ParamNames: []string{"stmt", "index", "value"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_db_bind_double",
				Doc:        "Bind a double value to a prepared statement parameter by index (1-based).",
			},
			"bindStr": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeInt, ast.TypeString},
				ParamNames: []string{"stmt", "index", "value"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_db_bind_str",
				Doc:        "Bind a string value to a prepared statement parameter by index (1-based).",
			},
			"step": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"stmt"},
				ReturnType: ast.TypeBool,
				CName:      "dex_db_step",
				Doc:        "Execute one step of a prepared statement. Returns true if a row is available.",
			},
			"reset": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"stmt"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_db_reset",
				Doc:        "Reset a prepared statement for re-execution with new parameter bindings.",
			},
			"finalize": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"stmt"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_db_finalize",
				Doc:        "Finalize and free a prepared statement.",
			},
		},
		CFlags:   detectDbFlags(),
		CRuntime: dbRuntime,
	})
}
