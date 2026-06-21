package stdlib

import (
	_ "embed"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/db.c
var dbRuntime string

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
		CFlags:   []string{"-lsqlite3", "-lpq", "-lmysqlclient", "-lmongoc-1.0", "-lbson-1.0"},
		CRuntime: dbRuntime,
	})
}
