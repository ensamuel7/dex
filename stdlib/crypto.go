package stdlib

import (
	_ "embed"
	"os/exec"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/crypto.c
var cryptoRuntimeBase string

//go:embed cruntime/crypto_digest.c
var cryptoDigestRuntime string

var cryptoRuntime = cryptoRuntimeBase + cryptoDigestRuntime

// SHA-256 and HMAC come from OpenSSL, which the ws module already links for
// wss://. Detected the same way, so a build either has both or neither rather
// than half-enabling one from a stray include path.
func detectCryptoFlags() []string {
	out, err := exec.Command("pkg-config", "--cflags", "openssl").Output()
	if err != nil {
		return nil
	}
	flags := []string{"-DDEX_HAS_SSL"}
	flags = append(flags, strings.Fields(strings.TrimSpace(string(out)))...)
	if libs, err := exec.Command("pkg-config", "--libs", "openssl").Output(); err == nil {
		flags = append(flags, strings.Fields(strings.TrimSpace(string(libs)))...)
	} else {
		flags = append(flags, "-lssl", "-lcrypto")
	}
	return flags
}

func init() {
	Register(&Module{
		Name: "crypto",
		Funcs: map[string]FuncDef{
			"uuid": {
				Params:     []ast.Type{},
				ReturnType: ast.TypeString,
				CName:      "dex_crypto_uuid",
				Doc:        "Generate a random UUID v4 string.",
			},
			"base64Encode": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"data"},
				ReturnType: ast.TypeString,
				CName:      "dex_crypto_base64_encode",
				RawParams:  []int{0},
				RawReturn:  true,
				Doc:        "Base64-encode a string. Binary-safe: NUL bytes are encoded, not truncated.",
			},
			"base64Decode": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"encoded"},
				ReturnType: ast.TypeString,
				CName:      "dex_crypto_base64_decode",
				RawParams:  []int{0},
				RawReturn:  true,
				Doc:        "Decode base64. Binary-safe: the result may contain NUL bytes.",
			},
			"sha256Hex": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"data"},
				ReturnType: ast.TypeString,
				CName:      "dex_crypto_sha256_hex",
				RawParams:  []int{0},
				RawReturn:  true,
				Doc:        "SHA-256 of a string, as lowercase hex. Needs OpenSSL.",
			},
			"hmacSha256Hex": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"key", "message"},
				ReturnType: ast.TypeString,
				CName:      "dex_crypto_hmac_sha256_hex",
				RawParams:  []int{0, 1},
				RawReturn:  true,
				Doc:        "HMAC-SHA-256, as lowercase hex. Needs OpenSSL.",
			},

			"hmacSha512Hex": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"key", "message"},
				ReturnType: ast.TypeString,
				CName:      "dex_crypto_hmac_sha512_hex",
				RawParams:  []int{0, 1},
				RawReturn:  true,
				Doc:        "HMAC-SHA512 of a message under a key, as lowercase hex. What Paystack signs webhooks with.",
			},
			"secureEquals": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"a", "b"},
				ReturnType: ast.TypeBool,
				CName:      "dex_crypto_secure_equals",
				// Both sides come through as DexString* so the compare can use
				// the stored length — a hex digest has no interior NUL, but a
				// token compared by strlen would stop at one.
				RawParams:  []int{0, 1},
				Doc:        "Compares two strings in time that does not depend on how much of a prefix matched. For signatures and tokens, where == leaks a prefix an attacker can walk.",
			},		},
		CFlags:   detectCryptoFlags(),
		CRuntime: cryptoRuntime,
	})
}
