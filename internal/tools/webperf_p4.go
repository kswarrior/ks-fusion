package tools

// P4 HMR perf: debounce + incremental caches + budgets logging.
//
// Debounce (1 render/tick): the SSE handler in webjs.go checks
// watcher.version() once per sseTickInterval tick; multiple .ks mtime
// bumps within the same tick window coalesce into a single
// renderRoute+Diff per subscribed route (last=ver after the tick).
// The per-route render cache below extends this: when the frontend
// mtime hash is unchanged (e.g. only backend files changed, or no
// change at all) the tick skips re-exec entirely (cache hit).
//
// Incremental parse: frontend .ks files are cached by mtime+size
// (map path -> {prog, mtime, size}); on each render only changed files
// are re-parsed, cached progs are reused for ExecProgram. New files
// miss and are parsed; deleted files are pruned from the cache.
//
// Content-hash analogue: per-route entries store {vmJSON, status,
// mtimeHash} where mtimeHash is sha256 over frontend file
// path+mtime+size. Unchanged routes skip re-render on tick.
//
// Poll/ticker values are kept (400ms poll / 300ms tick) but documented
// as constants with TODO for WS push.
//
// Budgets (plan/frontend.md:98-99): TTFR<1s, HMR<100ms,
// route JS 100KB warn / 250KB fail. TTFR is already emitted as the
// X-Render-Time header; slow HMR ticks (>100ms) log when
// FUSION_DEBUG_HMR=1 to avoid noise. BuildJS budgets live in
// buildjs_p3.go (warn>100KB/fail>250KB) and are unchanged.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kswarrior/ks-fusion/internal/frontend"
)

// HMR/dev polling intervals (kept values, documented).
// TODO: replace mtime poll + SSE ticker with WS push (Vite analogue).
const (
	// webWatchPollInterval polls .ks mtimes for --watch (was 400ms inline).
	webWatchPollInterval = 400 * time.Millisecond
	// sseTickInterval is the /events SSE tick (was 300ms inline).
	sseTickInterval = 300 * time.Millisecond
)

// Budgets (plan/frontend.md §7).
const (
	// hmrTickBudget is the HMR patch budget (<100ms).
	hmrTickBudget = 100 * time.Millisecond
	// ttfrBudget is the time-to-first-render budget (<1s local).
	ttfrBudget = time.Second
	// buildJSWarnBytes / buildJSFailBytes mirror buildjs_p3.go budgets.
	buildJSWarnBytes = 100 * 1024
	buildJSFailBytes = 250 * 1024
)

// --- incremental parse cache (mtime+size) ---

type cachedFrontendFile struct {
	prog  *frontend.Program
	mtime time.Time
	size  int64
}

var (
	frontendParseCacheMu     sync.Mutex
	frontendParseCache       = map[string]cachedFrontendFile{}
	frontendParseCacheHits   int
	frontendParseCacheMisses int
)

// getCachedFrontendProgram returns the parsed program for absPath,
// re-parsing only when mtime or size changed. Failures are never cached.
func getCachedFrontendProgram(absPath, display string) (*frontend.Program, error) {
	fi, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	mtime := fi.ModTime()
	size := fi.Size()
	frontendParseCacheMu.Lock()
	if e, ok := frontendParseCache[absPath]; ok && e.mtime.Equal(mtime) && e.size == size {
		frontendParseCacheHits++
		prog := e.prog
		frontendParseCacheMu.Unlock()
		return prog, nil
	}
	frontendParseCacheMu.Unlock()
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	prog, err := frontend.ParseSource(string(data), display)
	if err != nil {
		return nil, err
	}
	frontendParseCacheMu.Lock()
	frontendParseCache[absPath] = cachedFrontendFile{prog: prog, mtime: mtime, size: size}
	frontendParseCacheMisses++
	frontendParseCacheMu.Unlock()
	return prog, nil
}

// pruneFrontendParseCache drops cached entries under frontendDir that are
// no longer in live (handles deletes). live keys are absolute paths.
func pruneFrontendParseCache(frontendDir string, live map[string]bool) {
	frontendParseCacheMu.Lock()
	defer frontendParseCacheMu.Unlock()
	for p := range frontendParseCache {
		if !strings.HasPrefix(p, frontendDir) {
			continue
		}
		if !live[p] {
			delete(frontendParseCache, p)
		}
	}
}

// FrontendParseCacheStats reports parse-cache hits/misses (tests/bench).
func FrontendParseCacheStats() (hits, misses int) {
	frontendParseCacheMu.Lock()
	defer frontendParseCacheMu.Unlock()
	return frontendParseCacheHits, frontendParseCacheMisses
}

// --- per-route render cache (route -> {vmJSON, status, mtimeHash}) ---

type cachedRouteRender struct {
	vmJSON string
	status int
	hash   string
}

var (
	routeRenderCacheMu sync.Mutex
	routeRenderCache   = map[string]cachedRouteRender{}
	routeCacheHits     int
	routeCacheRenders  int
)

func routeCacheKey(appDir, route string) string { return appDir + "\x00" + route }

// listFrontendFiles returns sorted absolute frontend .ks paths.
func listFrontendFiles(appDir string) []string {
	var files []string
	_ = filepath.Walk(filepath.Join(appDir, "frontend"), func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".ks") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	return files
}

// frontendMtimeHash hashes path+mtime+size for files (already sorted).
// Deleted/unstatable files contribute a marker so the hash changes.
func frontendMtimeHash(files []string) string {
	h := sha256.New()
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil {
			fmt.Fprintf(h, "deleted:%s;", f)
			continue
		}
		fmt.Fprintf(h, "%s:%d:%d;", f, fi.ModTime().UnixNano(), fi.Size())
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:])
}

// execFrontendFiles filters out frontend/main.ks (entry route table, not
// executed — same rule as renderRoute).
func execFrontendFiles(appDir string, files []string) []string {
	mainKS := filepath.Join(appDir, "frontend", "main.ks")
	out := files[:0:0]
	for _, f := range files {
		if f == mainKS {
			continue
		}
		out = append(out, f)
	}
	return out
}

func routeCacheGet(appDir, route, hash string) (vmJSON string, status int, ok bool) {
	routeRenderCacheMu.Lock()
	defer routeRenderCacheMu.Unlock()
	e, ok := routeRenderCache[routeCacheKey(appDir, route)]
	if !ok || e.hash != hash {
		return "", 0, false
	}
	routeCacheHits++
	return e.vmJSON, e.status, true
}

func routeCachePut(appDir, route, hash, vmJSON string, status int) {
	routeRenderCacheMu.Lock()
	defer routeRenderCacheMu.Unlock()
	routeRenderCache[routeCacheKey(appDir, route)] = cachedRouteRender{vmJSON: vmJSON, status: status, hash: hash}
	routeCacheRenders++
}

// RouteCacheStats reports (hits, renders) for tests/bench.
func RouteCacheStats() (hits, renders int) {
	routeRenderCacheMu.Lock()
	defer routeRenderCacheMu.Unlock()
	return routeCacheHits, routeCacheRenders
}

// ResetFrontendCachesForTests clears parse + route caches and counters.
func ResetFrontendCachesForTests() {
	frontendParseCacheMu.Lock()
	frontendParseCache = map[string]cachedFrontendFile{}
	frontendParseCacheHits = 0
	frontendParseCacheMisses = 0
	frontendParseCacheMu.Unlock()
	routeRenderCacheMu.Lock()
	routeRenderCache = map[string]cachedRouteRender{}
	routeCacheHits = 0
	routeCacheRenders = 0
	routeRenderCacheMu.Unlock()
}

// --- budgets logging (gated to avoid noise) ---

func hmrDebugEnabled() bool { return os.Getenv("FUSION_DEBUG_HMR") == "1" }

// maybeLogSlowHMRTick logs HMR ticks exceeding the 100ms budget.
// Gated by FUSION_DEBUG_HMR=1; watch mode only.
func maybeLogSlowHMRTick(start time.Time, route string) {
	if !hmrDebugEnabled() {
		return
	}
	if el := time.Since(start); el > hmrTickBudget {
		fmt.Printf("hmr slow tick: route %s took %s (>100ms budget)\n", route, el)
	}
}

// maybeLogSlowTTFR logs first-render times exceeding the 1s budget.
// X-Render-Time header is always set; this printf is debug-only.
func maybeLogSlowTTFR(start time.Time, route string) {
	if !hmrDebugEnabled() {
		return
	}
	if el := time.Since(start); el > ttfrBudget {
		fmt.Printf("ttfr slow: route %s took %s (>1s budget)\n", route, el)
	}
}
