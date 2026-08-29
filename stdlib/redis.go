package stdlib

import (
	_ "embed"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/redis.c
var redisRuntime string

func init() {
	Register(&Module{
		Name: "redis",
		Funcs: map[string]FuncDef{
			"connect": {
				Params:     []ast.Type{ast.TypeString, ast.TypeInt, ast.TypeString},
				ParamNames: []string{"host", "port", "password"},
				ReturnType: ast.TypeInt,
				CName:      "dex_redis_connect",
				Doc:        "Open a connection pool to Redis. Returns a handle, or -1 if unreachable. Pass \"\" for no password.",
			},
			"close": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"conn"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_redis_close",
				Doc:        "Close every socket held by the handle.",
			},
			"ping": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"conn"},
				ReturnType: ast.TypeBool,
				CName:      "dex_redis_ping",
				Doc:        "True when the server answers.",
			},

			"set": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString, ast.TypeString},
				ParamNames: []string{"conn", "key", "value"},
				ReturnType: ast.TypeBool,
				CName:      "dex_redis_set",
				Doc:        "Set a key.",
			},
			"setex": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString, ast.TypeString, ast.TypeInt},
				ParamNames: []string{"conn", "key", "value", "seconds"},
				ReturnType: ast.TypeBool,
				CName:      "dex_redis_setex",
				Doc:        "Set a key that expires after the given number of seconds.",
			},
			"get": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString},
				ParamNames: []string{"conn", "key"},
				ReturnType: ast.TypeString,
				CName:      "dex_redis_get",
				Doc:        "Read a key. A missing key reads as the empty string.",
			},
			"exists": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString},
				ParamNames: []string{"conn", "key"},
				ReturnType: ast.TypeBool,
				CName:      "dex_redis_exists",
				Doc:        "True when the key is present.",
			},
			"del": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString},
				ParamNames: []string{"conn", "key"},
				ReturnType: ast.TypeLong,
				CName:      "dex_redis_del",
				Doc:        "Delete a key. Returns how many were removed.",
			},
			"expire": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString, ast.TypeInt},
				ParamNames: []string{"conn", "key", "seconds"},
				ReturnType: ast.TypeBool,
				CName:      "dex_redis_expire",
				Doc:        "Give an existing key a time to live.",
			},
			"ttl": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString},
				ParamNames: []string{"conn", "key"},
				ReturnType: ast.TypeLong,
				CName:      "dex_redis_ttl",
				Doc:        "Seconds until the key expires; -1 if it never does, -2 if it is gone.",
			},
			"incr": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString},
				ParamNames: []string{"conn", "key"},
				ReturnType: ast.TypeLong,
				CName:      "dex_redis_incr",
				Doc:        "Increment a counter and return its new value.",
			},
			"keys": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString},
				ParamNames: []string{"conn", "pattern"},
				ReturnType: ast.TypeArrayString,
				CName:      "dex_redis_keys",
				Doc:        "Keys matching a glob pattern. Scans the keyspace — not for hot paths.",
			},

			"hset": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString, ast.TypeString, ast.TypeString},
				ParamNames: []string{"conn", "key", "field", "value"},
				ReturnType: ast.TypeBool,
				CName:      "dex_redis_hset",
				Doc:        "Set one field of a hash.",
			},
			"hget": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString, ast.TypeString},
				ParamNames: []string{"conn", "key", "field"},
				ReturnType: ast.TypeString,
				CName:      "dex_redis_hget",
				Doc:        "Read one field of a hash. A missing field reads as the empty string.",
			},
			"hdel": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString, ast.TypeString},
				ParamNames: []string{"conn", "key", "field"},
				ReturnType: ast.TypeLong,
				CName:      "dex_redis_hdel",
				Doc:        "Remove one field of a hash.",
			},
			"hkeys": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString},
				ParamNames: []string{"conn", "key"},
				ReturnType: ast.TypeArrayString,
				CName:      "dex_redis_hkeys",
				Doc:        "The field names of a hash.",
			},
			"hgetall": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString},
				ParamNames: []string{"conn", "key"},
				ReturnType: ast.TypeArrayString,
				CName:      "dex_redis_hgetall",
				Doc:        "A hash as a flat array: field, value, field, value.",
			},

			"sadd": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString, ast.TypeString},
				ParamNames: []string{"conn", "key", "member"},
				ReturnType: ast.TypeLong,
				CName:      "dex_redis_sadd",
				Doc:        "Add a member to a set.",
			},
			"srem": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString, ast.TypeString},
				ParamNames: []string{"conn", "key", "member"},
				ReturnType: ast.TypeLong,
				CName:      "dex_redis_srem",
				Doc:        "Remove a member from a set.",
			},
			"sismember": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString, ast.TypeString},
				ParamNames: []string{"conn", "key", "member"},
				ReturnType: ast.TypeBool,
				CName:      "dex_redis_sismember",
				Doc:        "True when the member is in the set.",
			},
			"smembers": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString},
				ParamNames: []string{"conn", "key"},
				ReturnType: ast.TypeArrayString,
				CName:      "dex_redis_smembers",
				Doc:        "Every member of a set.",
			},

			"publish": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString, ast.TypeString},
				ParamNames: []string{"conn", "channel", "message"},
				ReturnType: ast.TypeLong,
				CName:      "dex_redis_publish",
				Doc:        "Publish to a channel. Returns how many subscribers received it.",
			},
			"subscribe": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeString},
				ParamNames: []string{"conn", "channel"},
				ReturnType: ast.TypeBool,
				CName:      "dex_redis_subscribe",
				Doc:        "Subscribe to a channel on a socket of its own. Call again for more channels.",
			},
			"nextMessage": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"conn"},
				ReturnType: ast.TypeString,
				CName:      "dex_redis_next_message",
				Doc:        "Block until the next published message and return it. Empty means the subscriber dropped.",
			},
			"lastChannel": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"conn"},
				ReturnType: ast.TypeString,
				CName:      "dex_redis_last_channel",
				Doc:        "The channel the last message arrived on.",
			},

			"command": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeArrayString},
				ParamNames: []string{"conn", "args"},
				ReturnType: ast.TypeString,
				CName:      "dex_redis_command",
				Doc:        "Run any command, spelled as its arguments. The reply is returned as text.",
			},
		},
		CRuntime: redisRuntime,
		CFlags:   []string{"-pthread"},
	})
}
