package wasmdemo

import (
	"encoding/base64"
	"path/filepath"
	"testing"

	wasmtime "github.com/bytecodealliance/wasmtime-go/v25"
)

// base64-spmd.wasm is compiled with -simd=true and uses the WASM
// "relaxed-simd" proposal (relaxed madd, for the FMA-style packing in the
// Mula-Lemire pipeline; see lanes.FMA / createSpmdFMA in the compiler).
// wazero (used for the other four demo binaries in main_test.go) does not
// implement the relaxed-simd proposal and fails to even decode this module
// ("type index out of range" — a relaxed-simd opcode being misparsed as a
// standard one). This is a pre-existing wazero limitation, not a build
// regression: the previously-shipped static/wasm/base64-spmd.wasm fails the
// exact same way under wazero. wasmtime, which the SPMD toolchain's own
// e2e/benchmark scripts already depend on (see test/e2e/*.sh in the main
// SPMD repo), does support relaxed-simd, so we use wasmtime-go here just for
// this one binary.
func TestBase64SPMDWasmtime(t *testing.T) {
	path := filepath.Join(wasmDir(t), "base64-spmd.wasm")

	engine := wasmtime.NewEngine()
	store := wasmtime.NewStore(engine)
	store.SetWasi(wasmtime.NewWasiConfig())

	linker := wasmtime.NewLinker(engine)
	if err := linker.DefineWasi(); err != nil {
		t.Fatalf("DefineWasi: %v", err)
	}

	module, err := wasmtime.NewModuleFromFile(engine, path)
	if err != nil {
		t.Fatalf("loading module %s: %v", path, err)
	}

	instance, err := linker.Instantiate(store, module)
	if err != nil {
		t.Fatalf("instantiating %s: %v", path, err)
	}

	// Run _start (TinyGo runtime init + main()); it calls proc_exit(0),
	// which wasmtime surfaces as a WASI exit trap. Same tolerated-trap
	// pattern as the shortcode's loadWasm() try/catch and as loadModule()
	// in main_test.go.
	start := instance.GetFunc(store, "_start")
	if start == nil {
		t.Fatalf("missing export _start")
	}
	// TinyGo WASI's _start always ends by calling proc_exit(0) after main()
	// returns, which wasmtime reports as an "Exited with i32 exit status 0"
	// error out of Call rather than a normal return. That's the expected,
	// successful-completion shape (mirrors the try/catch in the shortcode's
	// loadWasm()), so any error here is tolerated — the module is still
	// usable afterwards since WASI exit doesn't tear down the instance.
	_, _ = start.Call(store)

	requireWasmtimeExports(t, instance, store,
		"getInputPtr", "decodeBase64", "getOutputPtr", "getOutputLen")

	mem := instance.GetExport(store, "memory").Memory()
	memData := mem.UnsafeData(store)

	getInputPtr := instance.GetFunc(store, "getInputPtr")
	decodeBase64 := instance.GetFunc(store, "decodeBase64")
	getOutputPtr := instance.GetFunc(store, "getOutputPtr")
	getOutputLen := instance.GetFunc(store, "getOutputLen")

	input := base64TestInput(t)

	ptrRes, err := getInputPtr.Call(store)
	if err != nil {
		t.Fatalf("getInputPtr: %v", err)
	}
	inputPtr := ptrRes.(int32)
	copy(memData[inputPtr:], input)

	nRes, err := decodeBase64.Call(store, int32(len(input)))
	if err != nil {
		t.Fatalf("decodeBase64: %v", err)
	}
	n := nRes.(int32)
	if n < 0 {
		t.Fatalf("decodeBase64 returned error code %d for valid input", n)
	}

	outPtrRes, err := getOutputPtr.Call(store)
	if err != nil {
		t.Fatalf("getOutputPtr: %v", err)
	}
	outputPtr := outPtrRes.(int32)

	outLenRes, err := getOutputLen.Call(store)
	if err != nil {
		t.Fatalf("getOutputLen: %v", err)
	}
	outputLen := outLenRes.(int32)
	if outputLen != n {
		t.Fatalf("getOutputLen()=%d != decodeBase64 return %d", outputLen, n)
	}

	// Re-fetch memory slice: growth (unlikely here) can invalidate it.
	memData = mem.UnsafeData(store)
	got := make([]byte, outputLen)
	copy(got, memData[outputPtr:outputPtr+outputLen])

	want, err := base64.StdEncoding.DecodeString(string(input))
	if err != nil {
		t.Fatalf("reference stdlib decode failed: %v", err)
	}

	if string(got) != string(want) {
		t.Fatalf("decoded output mismatch for base64-spmd.wasm (relaxed-simd path):\n got (%d bytes) = %q...\nwant (%d bytes) = %q...",
			len(got), truncate(got, 64), len(want), truncate(want, 64))
	}
}

func requireWasmtimeExports(t *testing.T, instance *wasmtime.Instance, store *wasmtime.Store, names ...string) {
	t.Helper()
	for _, n := range names {
		if instance.GetExport(store, n) == nil {
			t.Fatalf("wasm module missing required export %q (shortcode contract broken)", n)
		}
	}
}
