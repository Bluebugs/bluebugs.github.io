# Build recipe for the interactive blog demo WASM binaries.
#
# These demos are compiled with the *forked* Go + TinyGo SPMD toolchain
# that lives in the sibling SPMD workspace (../../ from this repo's own
# checkout location, i.e. the SPMD superproject that has this repo as the
# `bluebugs.github.io` submodule). They are NOT buildable with a stock
# Go/TinyGo install because they use `lanes.Varying[T]`, `go for`, and
# other GOEXPERIMENT=spmd-gated syntax.
#
# Toolchain requirements (override via env/make vars if your checkout
# layout differs):
#   GOROOT_BIN  - bin/ dir of the forked Go (must be on PATH so TinyGo's
#                 `go env` finds the SPMD-patched go tool)
#   TINYGO      - path to the forked tinygo binary (build/tinygo)
#   WASMOPT     - path to a wasm-opt >= v109 (binaryen). The system
#                 package manager's wasm-opt is usually too old to
#                 understand the WASM instructions TinyGo emits here;
#                 use binaryen v119 (or newer) explicitly. This matches
#                 the version pinned by the SPMD playground's Docker image.
#
# Usage:
#   make -C examples \
#       GOROOT_BIN=/path/to/SPMD/go/bin \
#       TINYGO=/path/to/SPMD/tinygo/build/tinygo \
#       WASMOPT=/path/to/binaryen-version_119/bin/wasm-opt \
#       all
#
# With the conventional SPMD workspace layout (this repo checked out as
# the `bluebugs.github.io` submodule of the SPMD superproject), the
# defaults below just work:
#   make -C examples all

SPMD_ROOT   ?= $(abspath $(CURDIR)/../..)
GOROOT_BIN  ?= $(SPMD_ROOT)/go/bin
TINYGO      ?= $(SPMD_ROOT)/tinygo/build/tinygo
WASMOPT     ?=

OUT_DIR := ../static/wasm

export PATH := $(GOROOT_BIN):$(PATH)
export GOEXPERIMENT := spmd
ifneq ($(WASMOPT),)
export WASMOPT
endif

TINYGO_BUILD := $(TINYGO) build -target=wasi

.PHONY: all clean check-tools \
	base64-spmd base64-scalar base64-stdlib \
	mandelbrot-spmd mandelbrot-serial

all: check-tools \
	base64-spmd base64-scalar base64-stdlib \
	mandelbrot-spmd mandelbrot-serial

check-tools:
	@test -x "$(TINYGO)" || { echo "error: TINYGO not found/executable: $(TINYGO)"; exit 1; }
	@test -x "$(GOROOT_BIN)/go" || { echo "error: forked go not found/executable: $(GOROOT_BIN)/go"; exit 1; }
	@if [ -n "$(WASMOPT)" ]; then \
		test -x "$(WASMOPT)" || { echo "error: WASMOPT not found/executable: $(WASMOPT)"; exit 1; }; \
	else \
		echo "warning: WASMOPT not set — falling back to whatever wasm-opt tinygo finds on PATH," \
		     "which is likely too old (see header comment). Set WASMOPT explicitly." >&2; \
	fi

# -- base64 decoder demo --
# base64_spmd.go is compiled twice: once with SIMD (Mula-Lemire vectorized
# kernel) and once with -simd=false (same source, scalar codegen) — see
# examples/base64_spmd.go's chunkSize comment for why the same source works
# in both modes.
base64-spmd: check-tools
	$(TINYGO_BUILD) -simd=true  -o $(OUT_DIR)/base64-spmd.wasm   base64_spmd.go

base64-scalar: check-tools
	$(TINYGO_BUILD) -simd=false -o $(OUT_DIR)/base64-scalar.wasm base64_spmd.go

base64-stdlib: check-tools
	$(TINYGO_BUILD) -o $(OUT_DIR)/base64-stdlib.wasm base64_stdlib.go

# -- mandelbrot demo --
mandelbrot-spmd: check-tools
	$(TINYGO_BUILD) -simd=true -o $(OUT_DIR)/mandelbrot-spmd.wasm   mandelbrot_spmd.go

mandelbrot-serial: check-tools
	$(TINYGO_BUILD) -o $(OUT_DIR)/mandelbrot-serial.wasm mandelbrot_serial.go

clean:
	rm -f $(OUT_DIR)/base64-spmd.wasm $(OUT_DIR)/base64-scalar.wasm $(OUT_DIR)/base64-stdlib.wasm \
	      $(OUT_DIR)/mandelbrot-spmd.wasm $(OUT_DIR)/mandelbrot-serial.wasm

# -- correctness tests (wazero/wasmtime-driven) --
# Run after `all` to verify the freshly built binaries against the
# shortcode's export/memory contract. See ../tests/wasmdemo.
.PHONY: test
test: all
	cd ../tests/wasmdemo && WASM_DIR=$(abspath $(OUT_DIR)) go test ./... -v
