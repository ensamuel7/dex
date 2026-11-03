package stdlib

import (
	_ "embed"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/file.c
var fileRuntimeBase string

//go:embed cruntime/file_bytes.c
var fileBytesRuntime string

//go:embed cruntime/file_meta.c
var fileMetaRuntime string

var fileRuntime = fileRuntimeBase + fileBytesRuntime + fileMetaRuntime

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
			"chmod": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"path", "mode"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_chmod",
				Doc:        "Set a file's permissions from an octal string such as \"0644\". A mode that is not 1-4 octal digits is refused.",
			},
			"chown": {
				Params:     []ast.Type{ast.TypeString, ast.TypeInt, ast.TypeInt},
				ParamNames: []string{"path", "uid", "gid"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_chown",
				Doc:        "Set a file's owner and group (-1 leaves one unchanged). A symbolic link is changed itself, never followed.",
			},
			"mode": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"path"},
				ReturnType: ast.TypeString,
				CName:      "dex_file_mode",
				Doc:        "A file's permissions as four octal digits, such as \"0644\", or \"\" if it does not exist. Does not follow a symbolic link.",
			},
			"uid": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"path"},
				ReturnType: ast.TypeInt,
				CName:      "dex_file_uid",
				Doc:        "The user id owning a file, or -1 if it does not exist. Does not follow a symbolic link.",
			},
			"gid": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"path"},
				ReturnType: ast.TypeInt,
				CName:      "dex_file_gid",
				Doc:        "The group id owning a file, or -1 if it does not exist. Does not follow a symbolic link.",
			},
			"isDir": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"path"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_is_dir",
				Doc:        "Whether path is a directory. A symbolic link to one is not.",
			},
			"isLink": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"path"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_is_link",
				Doc:        "Whether path is a symbolic link.",
			},
			"symlink": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"target", "link"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_symlink",
				Doc:        "Create a symbolic link at link pointing to target.",
			},
			"readlink": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"path"},
				ReturnType: ast.TypeString,
				CName:      "dex_file_readlink",
				Doc:        "The target a symbolic link points to, or \"\" if path is not one.",
			},
			"removeAll": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"path"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_remove_all",
				Doc:        "Remove a file or a whole directory tree, never following a link inside it. True if path is gone, including when it never existed. Refuses \"\" and \"/\".",
			},
			"writeAtomic": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString, ast.TypeString},
				ParamNames: []string{"path", "content", "mode"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_write_atomic",
				RawParams:  []int{1},
				Doc:        "Write a file through a temporary beside it renamed into place, with the octal mode set first: readers see the old contents or the new, never part. Binary-safe.",
			},
			"lock": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"path"},
				ReturnType: ast.TypeInt,
				CName:      "dex_file_lock",
				Doc:        "Take an exclusive lock on path (created if missing), waiting until it is free. Returns a handle for file.unlock, or -1.",
			},
			"unlock": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"handle"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_unlock",
				Doc:        "Release a lock taken by file.lock.",
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
