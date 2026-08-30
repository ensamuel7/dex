package stdlib

import (
	_ "embed"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/file.c
var fileRuntimeBase string

//go:embed cruntime/file_bytes.c
var fileBytesRuntime string

var fileRuntime = fileRuntimeBase + fileBytesRuntime

func init() {
	Register(&Module{
		Name: "file",
		Funcs: map[string]FuncDef{
			"read": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"path"},
				ReturnType: ast.TypeString,
				CName:      "dex_file_read",
				Doc:        "Read the entire contents of a file as a string.",
			},
			"write": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"path", "content"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_write",
				Doc:        "Write content to a file, overwriting if it exists.",
			},
			"append": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"path", "content"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_append",
				Doc:        "Append content to the end of a file.",
			},
			"list": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"path"},
				ReturnType: ast.TypeArrayString,
				CName:      "dex_file_list",
				Doc:        "The entries of a directory, sorted by name, without \".\" or \"..\". A path that cannot be read reads as empty.",
			},
			"exists": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"path"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_exists",
				Doc:        "Check if a file exists at the given path.",
			},
			"remove": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"path"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_remove",
				Doc:        "Delete a file at the given path.",
			},
			"rename": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"oldPath", "newPath"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_rename",
				Doc:        "Rename a file from oldPath to newPath.",
			},
			"readBytes": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"path"},
				ReturnType: ast.TypeString,
				CName:      "dex_file_read_bytes",
				RawReturn:  true,
				Doc:        "Read a file's bytes. Binary-safe: NUL bytes are preserved.",
			},
			"writeBytes": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"path", "content"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_write_bytes",
				RawParams:  []int{1},
				Doc:        "Write bytes to a file, overwriting. Binary-safe.",
			},
			"appendBytes": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"path", "content"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_append_bytes",
				RawParams:  []int{1},
				Doc:        "Append bytes to a file. Binary-safe.",
			},
			"size": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"path"},
				ReturnType: ast.TypeInt,
				CName:      "dex_file_size",
				Doc:        "Size of a file in bytes, or -1 if it cannot be read.",
			},
			"mkdirp": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"path"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_mkdirp",
				Doc:        "Create a directory and any missing parents.",
			},
			"sha256Hex": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"path"},
				ReturnType: ast.TypeString,
				CName:      "dex_file_sha256_hex",
				RawReturn:  true,
				Doc:        "SHA-256 of a file's contents, as lowercase hex. Needs OpenSSL.",
			},
			"putUrl": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString, ast.TypeString},
				ParamNames: []string{"path", "url", "headers"},
				ReturnType: ast.TypeInt,
				CName:      "dex_file_put_url",
				Doc:        "PUT a file's bytes to a URL with newline-separated headers. Returns the HTTP status.",
			},
			"move": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"src", "dst"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_rename",
				Doc:        "Move a file from src to dst.",
			},
		},
		CFlags:   append([]string{"-lcurl"}, detectCryptoFlags()...),
		CRuntime: fileRuntime,
	})
}
