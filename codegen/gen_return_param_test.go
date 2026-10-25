package codegen

import (
	"strings"
	"testing"
)

// A by-value parameter returned whole from a function with heap locals of its
// own is the caller's, so the copy handed back takes its own references. It
// was treated as a local whose ownership moves, and the caller then released
// the struct it passed in and the one it got back, one set of strings between
// them.
func TestReturnedParameterRetainsItsFields(t *testing.T) {
	src := `
struct Report {
    name: string
}

fn stamped(r: Report): Report {
    let note: string = "x" + r.name
    return r
}

fn main(): void {
    let a: Report = Report { name: "a" }
    let b: Report = stamped(a)
}
`
	c := generate(t, src)
	start := strings.Index(c, "Dex_Report stamped(")
	if start < 0 {
		t.Fatalf("stamped not generated:\n%s", c)
	}
	body := c[start:]
	body = body[:strings.Index(body, "\n}\n")]
	if !strings.Contains(body, "dex_retain(_ret_tmp.name)") {
		t.Errorf("returned parameter's field is not retained:\n%s", body)
	}
}

// os.exit never returns, so the frame's locals are released before it rather
// than left to a cleanup that never runs.
func TestExitReleasesTheFrameFirst(t *testing.T) {
	src := `
import "os"
import "str"

fn main(): void {
    let s: string = str.fromInt(4)
    os.exit(0)
}
`
	c := generate(t, src)
	exit := strings.Index(c, "dex_os_exit(_dex_exit_code)")
	release := strings.Index(c, "dex_release(s)")
	if exit < 0 || release < 0 || release > exit {
		t.Errorf("s is not released before os.exit:\n%s", c)
	}
}
