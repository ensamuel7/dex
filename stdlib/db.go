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
				ReturnType: ast.TypeInt,
				CName:      "dex_db_open",
			},
			"exec": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString},
				ReturnType: ast.TypeInt,
				CName:      "dex_db_exec",
			},
			"query": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString},
				ReturnType: ast.TypeInt,
				CName:      "dex_db_query",
			},
			"next": {
				Params:     []ast.Type{ast.TypeInt},
				ReturnType: ast.TypeBool,
				CName:      "dex_db_next",
			},
			"col": {
				Params:     nil, // polymorphic — return type resolved from context
				ReturnType: ast.TypeInt,
				CName:      "",
			},
			"free": {
				Params:     []ast.Type{ast.TypeInt},
				ReturnType: ast.TypeVoid,
				CName:      "dex_db_free",
			},
			"close": {
				Params:     []ast.Type{ast.TypeInt},
				ReturnType: ast.TypeVoid,
				CName:      "dex_db_close",
			},
		},
		CIncludes: `#include <stdio.h>
		#include <stdlib.h>
		#include <string.h>
		#include <sqlite3.h>
		#include <libpq-fe.h>
		#include <mysql/mysql.h>
		#include <mongoc/mongoc.h>
		`,
		CFlags:   []string{"-lsqlite3", "-lpq", "-lmysqlclient", "-lmongoc-1.0", "-lbson-1.0"},
		CRuntime: dbRuntime,
	})
}
