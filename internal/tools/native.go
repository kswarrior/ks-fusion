package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kswarrior/ks-fusion/internal/native"
)

// ---------------------------------------------------------------------------
// fusion native (real machine code via Go codegen, native-0.1)
// ---------------------------------------------------------------------------

// BuildNative transpiles a single .ks file in the native strict subset to
// Go and compiles it to a real machine-code binary with `go build`.
// With emit=true it prints the generated Go source instead of building.
func BuildNative(srcPath, out, target string, strip, emit bool) error {
	goSrc, err := native.TranspileToGo(srcPath)
	if err != nil {
		return err
	}
	if emit {
		fmt.Println(goSrc)
		return nil
	}
	modRoot := findModuleRoot()
	tmpName := fmt.Sprintf("tmp-fusion-native-%d", time.Now().UnixNano())
	tmpDir := filepath.Join(modRoot, tmpName)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(goSrc), 0o644); err != nil {
		return err
	}
	if out == "" {
		out = strings.TrimSuffix(filepath.Base(srcPath), ".ks")
		if out == "" || out == filepath.Base(srcPath) {
			out = "native-out"
		}
		if strings.Contains(target, "windows") {
			out += ".exe"
		}
	}
	absOut, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	args := []string{"build", "-trimpath"}
	if strip {
		args = append(args, "-ldflags", "-s -w")
	}
	args = append(args, "-o", absOut, "./"+tmpName)
	cmd := exec.Command("go", args...)
	cmd.Dir = modRoot
	env := os.Environ()
	env = append(env, "GOFLAGS=-trimpath", "CGO_ENABLED=0")
	if target != "" && target != "host" {
		goos, goarch := parseTarget(target)
		if goos == "" {
			return fmt.Errorf("bad --target %q (want linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64, wasm)", target)
		}
		env = append(env, "GOOS="+goos, "GOARCH="+goarch)
		if goos == "js" {
			env = append(env, "GOOS=js", "GOARCH=wasm")
		}
		env = append(env, "CGO_ENABLED=0")
	}
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build failed: %w", err)
	}
	fmt.Printf("built native: %s (target %s, subset native-%s)\n", absOut, targetOrHost(target), native.Version)
	return nil
}
