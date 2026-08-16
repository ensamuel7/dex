package stdlib

import (
	_ "embed"
	"os/exec"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/db.c
var dbRuntime string

func detectDbFlags() []string {
	flags := []string{"-lsqlite3"}

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
		},
		CFlags:   detectDbFlags(),
		CRuntime: dbRuntime,
	})
}
