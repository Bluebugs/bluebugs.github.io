// Package wasmdemo hosts wazero-based correctness tests for the interactive
// blog demo WASM binaries (base64 decoder, Mandelbrot renderer). These
// binaries are built with -target=wasi from bluebugs.github.io/examples and
// export plain functions (not the js/wasm "syscall/js" ABI), so they can be
// driven directly with wazero + wasi_snapshot_preview1, without needing
// wasm_exec.js or a browser.
//
// The exported-function contract mirrors what layouts/shortcodes/spmd-*.html
// rely on:
//
//	base64: getInputPtr() i32; JS writes input bytes at that offset;
//	        decodeBase64(inputLen i32) i32; getOutputPtr() i32; getOutputLen() i32
//	mandelbrot: computeMandelbrot(width,height,maxIter i32)
//	            computeMandelbrotZoom(x0,y0,x1,y1 f32, width,height,maxIter i32)
//	            getBufferPtr() i32; getBufferLen() i32 (buffer is int32 per pixel)
package wasmdemo

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// wasmDir points at the built demo binaries. Overridable via WASM_DIR env
// var so CI/dev can point at static/wasm.new (rebuilt output) vs static/wasm
// (currently shipped output).
func wasmDir(t *testing.T) string {
	t.Helper()
	if d := os.Getenv("WASM_DIR"); d != "" {
		return d
	}
	return filepath.Join("..", "..", "static", "wasm")
}

// loadModule instantiates a WASI-target TinyGo wasm binary, runs _start
// (tolerating the expected proc_exit trap), and returns the module handle
// with its exports ready to call.
func loadModule(t *testing.T, path string) api.Module {
	t.Helper()
	ctx := context.Background()

	wasmBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	rt := wazero.NewRuntime(ctx)
	t.Cleanup(func() { rt.Close(ctx) })

	// Build the wasi_snapshot_preview1 host module ourselves rather than via
	// wasi_snapshot_preview1.Instantiate, so we can override proc_exit: the
	// stock implementation calls mod.CloseWithExitCode, which would make the
	// module's exports (getInputPtr, decodeBase64, ...) permanently
	// unusable right after the demo's _start() finishes initializing. The
	// browser's wasm_exec.js glue doesn't have this problem because
	// WebAssembly instances have no such "closed" state in the JS API — the
	// try/catch around _start() there is only about unwinding the JS stack
	// past the trap, not about tearing down the instance. We replicate that:
	// record the exit code but leave the module instance alive.
	wasiBuilder := rt.NewHostModuleBuilder(wasi_snapshot_preview1.ModuleName)
	wasi_snapshot_preview1.NewFunctionExporter().ExportFunctions(wasiBuilder)
	wasiBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, exitCode uint32) {
			// Intentionally a no-op beyond recording nothing: TinyGo's
			// runtime has already finished global init and main() by the
			// time proc_exit(0) is called, which is all we need.
		}).
		Export("proc_exit")
	if _, err := wasiBuilder.Instantiate(ctx); err != nil {
		t.Fatalf("instantiating wasi: %v", err)
	}

	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compiling %s: %v", path, err)
	}

	cfg := wazero.NewModuleConfig().WithStartFunctions() // don't auto-run _start; we call it explicitly below
	mod, err := rt.InstantiateModule(ctx, compiled, cfg)
	if err != nil {
		t.Fatalf("instantiating %s: %v", path, err)
	}

	// TinyGo WASI modules export _start, which calls proc_exit(0) after
	// main() returns; wazero surfaces that as a sys.ExitError. That's the
	// expected way for a WASI "command" module to finish initialization,
	// mirroring the try/catch around inst.exports._start() in the
	// shortcodes' loadWasm().
	if start := mod.ExportedFunction("_start"); start != nil {
		_, _ = start.Call(ctx) // ignore expected proc_exit error
	}

	return mod
}

// call0 invokes a zero/one-arg-returning-i32 export with no args.
func callI32(t *testing.T, ctx context.Context, mod api.Module, name string, args ...uint64) uint64 {
	t.Helper()
	fn := mod.ExportedFunction(name)
	if fn == nil {
		t.Fatalf("missing export %q", name)
	}
	res, err := fn.Call(ctx, args...)
	if err != nil {
		t.Fatalf("calling %s: %v", name, err)
	}
	if len(res) == 0 {
		return 0
	}
	return res[0]
}

// requireExports fails the test if any of the given export names is absent,
// which is how we detect an export-signature/name drift against the
// shortcode's expectations (see spmd-base64.html / spmd-mandelbrot.html).
func requireExports(t *testing.T, mod api.Module, names ...string) {
	t.Helper()
	for _, n := range names {
		if mod.ExportedFunction(n) == nil {
			t.Fatalf("wasm module missing required export %q (shortcode contract broken)", n)
		}
	}
}

// --- base64 demo ---

// base64TestInput mirrors what layouts/shortcodes/spmd-base64.html feeds the
// wasm: btoa(plaintext) i.e. standard padded base64 of ASCII text, truncated
// to a multiple of 4 characters, sent as raw ASCII bytes.
func base64TestInput(t *testing.T) []byte {
	t.Helper()
	plain := ""
	src := "The quick brown fox jumps over the lazy dog. Go was designed at Google in 2007. "
	for len(plain) < 5000 {
		plain += src
	}
	enc := base64.StdEncoding.EncodeToString([]byte(plain))
	enc = enc[:len(enc)-(len(enc)%4)]
	return []byte(enc)
}

func testBase64Demo(t *testing.T, wasmFile string) {
	ctx := context.Background()
	mod := loadModule(t, filepath.Join(wasmDir(t), wasmFile))
	requireExports(t, mod, "getInputPtr", "decodeBase64", "getOutputPtr", "getOutputLen")

	input := base64TestInput(t)

	inputPtr := uint32(callI32(t, ctx, mod, "getInputPtr"))
	mem := mod.Memory()
	if ok := mem.Write(inputPtr, input); !ok {
		t.Fatalf("writing input bytes at ptr %d (len %d) failed — out of bounds?", inputPtr, len(input))
	}

	n := int32(callI32(t, ctx, mod, "decodeBase64", api.EncodeI32(int32(len(input)))))
	if n < 0 {
		t.Fatalf("decodeBase64 returned error code %d for valid input", n)
	}

	outputPtr := uint32(callI32(t, ctx, mod, "getOutputPtr"))
	outputLen := uint32(callI32(t, ctx, mod, "getOutputLen"))
	if int32(outputLen) != n {
		t.Fatalf("getOutputLen()=%d != decodeBase64 return %d", outputLen, n)
	}

	got, ok := mem.Read(outputPtr, outputLen)
	if !ok {
		t.Fatalf("reading output bytes at ptr %d len %d failed", outputPtr, outputLen)
	}

	want, err := base64.StdEncoding.DecodeString(string(input))
	if err != nil {
		t.Fatalf("reference stdlib decode failed: %v", err)
	}

	if string(got) != string(want) {
		t.Fatalf("decoded output mismatch for %s:\n got (%d bytes) = %q...\nwant (%d bytes) = %q...",
			wasmFile, len(got), truncate(got, 64), len(want), truncate(want, 64))
	}
}

func truncate(b []byte, n int) []byte {
	if len(b) > n {
		return b[:n]
	}
	return b
}

func TestBase64Stdlib(t *testing.T) { testBase64Demo(t, "base64-stdlib.wasm") }
func TestBase64Scalar(t *testing.T) { testBase64Demo(t, "base64-scalar.wasm") }

// base64-spmd.wasm is covered by TestBase64SPMDWasmtime (base64_spmd_wasmtime_test.go),
// not here: it uses WASM relaxed-simd instructions that wazero cannot decode
// ("type index out of range"). This is a pre-existing wazero limitation —
// the previously-shipped static/wasm/base64-spmd.wasm fails identically —
// not something introduced by rebuilding with the current SPMD toolchain.

// --- mandelbrot demo ---

// mandelbrotViewport mirrors the shortcode's default viewport
// (mandelbrotSPMD default) and one zoomed frame from computeViewport().
type mandelbrotViewport struct {
	name                   string
	x0, y0, x1, y1         float32
	width, height, maxIter int32
}

var mandelbrotViewports = []mandelbrotViewport{
	{"default", -2.5, -1.25, 1.5, 1.25, 360, 300, 256},
	{"zoomed-seahorse", -0.75 - 2.0*0.5, 0.1 - 1.25*0.5, -0.75 + 2.0*0.5, 0.1 + 1.25*0.5, 360, 300, 256},
}

func computeMandelbrotBuffer(t *testing.T, wasmFile string, vp mandelbrotViewport) []int32 {
	ctx := context.Background()
	mod := loadModule(t, filepath.Join(wasmDir(t), wasmFile))
	requireExports(t, mod, "computeMandelbrotZoom", "computeMandelbrot", "getBufferPtr", "getBufferLen")

	fn := mod.ExportedFunction("computeMandelbrotZoom")
	_, err := fn.Call(ctx,
		api.EncodeF32(vp.x0), api.EncodeF32(vp.y0), api.EncodeF32(vp.x1), api.EncodeF32(vp.y1),
		api.EncodeI32(vp.width), api.EncodeI32(vp.height), api.EncodeI32(vp.maxIter),
	)
	if err != nil {
		t.Fatalf("computeMandelbrotZoom: %v", err)
	}

	ptr := uint32(callI32(t, ctx, mod, "getBufferPtr"))
	pixels := uint32(vp.width) * uint32(vp.height)
	raw, ok := mod.Memory().Read(ptr, pixels*4)
	if !ok {
		t.Fatalf("reading buffer at ptr %d, %d pixels", ptr, pixels)
	}

	out := make([]int32, pixels)
	for i := range out {
		out[i] = int32(api.DecodeI32(uint64(
			uint32(raw[i*4]) | uint32(raw[i*4+1])<<8 | uint32(raw[i*4+2])<<16 | uint32(raw[i*4+3])<<24,
		)))
	}
	return out
}

// TestMandelbrotSPMDMatchesSerial cross-checks the SPMD and serial kernels
// on identical viewports: since both implement the exact same math
// (mandelSPMD vs mandelSerial), their iteration-count buffers must be
// pixel-for-pixel identical. This is a strong, self-contained correctness
// test that needs no external oracle.
func TestMandelbrotSPMDMatchesSerial(t *testing.T) {
	for _, vp := range mandelbrotViewports {
		vp := vp
		t.Run(vp.name, func(t *testing.T) {
			serial := computeMandelbrotBuffer(t, "mandelbrot-serial.wasm", vp)
			spmd := computeMandelbrotBuffer(t, "mandelbrot-spmd.wasm", vp)

			if len(serial) != len(spmd) {
				t.Fatalf("buffer length mismatch: serial=%d spmd=%d", len(serial), len(spmd))
			}
			mismatches := 0
			for i := range serial {
				if serial[i] != spmd[i] {
					mismatches++
					if mismatches <= 5 {
						x := i % int(vp.width)
						y := i / int(vp.width)
						t.Errorf("pixel (%d,%d): serial=%d spmd=%d", x, y, serial[i], spmd[i])
					}
				}
			}
			if mismatches > 0 {
				t.Fatalf("%d/%d pixels mismatched between serial and spmd kernels", mismatches, len(serial))
			}
		})
	}
}

// TestMandelbrotSanity checks the buffers aren't degenerate (all zero / all
// maxIter), which would silently pass the equality check above while both
// kernels are simultaneously broken.
func TestMandelbrotSanity(t *testing.T) {
	vp := mandelbrotViewports[0]
	for _, wasmFile := range []string{"mandelbrot-serial.wasm", "mandelbrot-spmd.wasm"} {
		wasmFile := wasmFile
		t.Run(wasmFile, func(t *testing.T) {
			buf := computeMandelbrotBuffer(t, wasmFile, vp)
			var zero, atMax int
			for _, v := range buf {
				if v == 0 {
					zero++
				}
				if v >= vp.maxIter {
					atMax++
				}
			}
			total := len(buf)
			if zero == total || atMax == total {
				t.Fatalf("%s produced a degenerate buffer (zero=%d atMax=%d total=%d)", wasmFile, zero, atMax, total)
			}
		})
	}
}
