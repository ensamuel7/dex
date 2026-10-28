package codegen

import "testing"

func TestCodegenHttpServerIsMultiThreaded(t *testing.T) {
	out := generate(t, `import "http"
fn hello(req: http.HttpRequest): http.HttpResponse {
    return http.response(200, "ok", "text/plain")
}
fn main(): void {
    http.route("GET", "/", hello)
    http.listen(3000)
}`)
	assertNotContains(t, out, "#define DEX_SINGLE_THREADED")
}

func TestCodegenWsIsMultiThreaded(t *testing.T) {
	out := generate(t, `import "ws"
fn main(): void {
    let s: string = "x"
}`)
	assertNotContains(t, out, "#define DEX_SINGLE_THREADED")
}

func TestCodegenTimerCallbackIsMultiThreaded(t *testing.T) {
	out := generate(t, `import "time"
import "fmt"
fn tick(): void {
    fmt.println("tick")
}
fn main(): void {
    time.setTimeout(tick, 10)
}`)
	assertNotContains(t, out, "#define DEX_SINGLE_THREADED")
}

func TestCodegenClockOnlyStaysSingleThreaded(t *testing.T) {
	out := generate(t, `import "time"
import "fmt"
fn main(): void {
    let s: string = "now ${time.unixNow()}"
    fmt.println(s)
}`)
	assertContains(t, out, "#define DEX_SINGLE_THREADED")
}
