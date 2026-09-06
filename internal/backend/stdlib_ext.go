package backend

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// extraBuiltins returns v2.2 stdlib additions (http, regex, crypto, fs, process, time, db, log, concurrency, types).
func extraBuiltins() []*BuiltinObj {
	return []*BuiltinObj{
		{Name: "http_get", Fn: bHttpGet},
		{Name: "http_post", Fn: bHttpPost},
		{Name: "fetch_json", Fn: bFetchJSON},
		{Name: "http_serve", Fn: bHttpServe},
		{Name: "regex_match", Fn: bRegexMatch},
		{Name: "regex_find", Fn: bRegexFind},
		{Name: "regex_replace", Fn: bRegexReplace},
		{Name: "regex_split", Fn: bRegexSplit},
		{Name: "sha256", Fn: bSha256},
		{Name: "md5", Fn: bMd5},
		{Name: "hmac_sha256", Fn: bHmacSha256},
		{Name: "base64_encode", Fn: bBase64Encode},
		{Name: "base64_decode", Fn: bBase64Decode},
		{Name: "hex_encode", Fn: bHexEncode},
		{Name: "hex_decode", Fn: bHexDecode},
		{Name: "uuid", Fn: bUUID},
		{Name: "random_bytes", Fn: bRandomBytes},
		{Name: "stat", Fn: bStat},
		{Name: "cp", Fn: bCp},
		{Name: "mv", Fn: bMv},
		{Name: "copy", Fn: bCp},
		{Name: "glob", Fn: bGlob},
		{Name: "path_join", Fn: bPathJoin},
		{Name: "abs_path", Fn: bAbsPath},
		{Name: "remove", Fn: bRemove},
		{Name: "exec", Fn: bExec},
		{Name: "shell", Fn: bShell},
		{Name: "cwd", Fn: bCwd},
		{Name: "env_all", Fn: bEnvAll},
		{Name: "format_time", Fn: bFormatTime},
		{Name: "parse_time", Fn: bParseTime},
		{Name: "time_parts", Fn: bTimeParts},
		{Name: "db_put", Fn: bDbPut},
		{Name: "db_get", Fn: bDbGet},
		{Name: "db_delete", Fn: bDbDelete},
		{Name: "db_list", Fn: bDbList},
		{Name: "log_info", Fn: bLogInfo},
		{Name: "log_warn", Fn: bLogWarn},
		{Name: "log_error", Fn: bLogError},
		{Name: "assert_eq", Fn: bAssertEq},
		{Name: "assert_ne", Fn: bAssertNe},
		{Name: "assert_contains", Fn: bAssertContains},
		{Name: "with_timeout", Fn: bWithTimeout},
		{Name: "parallel", Fn: bParallel},
		{Name: "struct_validate", Fn: bStructValidate},
		{Name: "struct_assert", Fn: bStructAssert},
		{Name: "enum_create", Fn: bEnumCreate},
		{Name: "enum_valid", Fn: bEnumValid},
		{Name: "is_number", Fn: bIsNumber},
		{Name: "trim_prefix", Fn: bTrimPrefix},
		{Name: "trim_suffix", Fn: bTrimSuffix},
		{Name: "repeat_str", Fn: bRepeatStr},
	}
}

// --- http ---

var httpClient = &http.Client{Timeout: 15 * time.Second}

func bHttpGet(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("http_get", args, 1, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("http_get wants url string, got %s", TypeName(args[0]))
	}
	url := args[0].Str
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return Nil(), err
	}
	if len(args) == 2 {
		if args[1].Kind != VMap {
			return Nil(), fmt.Errorf("http_get headers must be map, got %s", TypeName(args[1]))
		}
		args[1].Map.Mu.RLock()
		for k, v := range args[1].Map.Vals {
			req.Header.Set(k, v.Display())
		}
		args[1].Map.Mu.RUnlock()
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return Nil(), err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return Nil(), err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Nil(), fmt.Errorf("http_get %s: status %d: %s", url, resp.StatusCode, string(data))
	}
	return StrV(string(data)), nil
}

func bHttpPost(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("http_post", args, 2, 3); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString || args[1].Kind != VString {
		return Nil(), fmt.Errorf("http_post wants (url, body) strings")
	}
	ct := "application/json"
	if len(args) == 3 {
		if args[2].Kind != VString {
			return Nil(), fmt.Errorf("http_post content-type must be string")
		}
		ct = args[2].Str
	}
	resp, err := httpClient.Post(args[0].Str, ct, strings.NewReader(args[1].Str))
	if err != nil {
		return Nil(), err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return Nil(), err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Nil(), fmt.Errorf("http_post %s: status %d: %s", args[0].Str, resp.StatusCode, string(data))
	}
	return StrV(string(data)), nil
}

func bFetchJSON(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("fetch_json", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("fetch_json wants url string, got %s", TypeName(args[0]))
	}
	s, err := bHttpGet(in, []Value{args[0]})
	if err != nil {
		return Nil(), err
	}
	return bJsonParse(in, []Value{s})
}

// http_serve(port, handler): handler is func(path:string)->string. Serves in background, returns port.
func bHttpServe(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("http_serve", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VInt {
		return Nil(), fmt.Errorf("http_serve port must be int, got %s", TypeName(args[0]))
	}
	if args[1].Kind != VFunc && args[1].Kind != VBuiltin {
		return Nil(), fmt.Errorf("http_serve handler must be func, got %s", TypeName(args[1]))
	}
	port := args[0].Int
	handler := args[1]
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		v, err := in.callValue(handler, []Value{StrV(r.URL.Path)})
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if v.Kind == VString {
			_, _ = w.Write([]byte(v.Str))
			return
		}
		j, err := valueToJSON(v)
		if err != nil {
			_, _ = w.Write([]byte(v.Display()))
			return
		}
		data, _ := json.Marshal(j)
		_, _ = w.Write(data)
	})
	srv := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux}
	go func() { _ = srv.ListenAndServe() }()
	// give it a moment to bind
	time.Sleep(50 * time.Millisecond)
	return IntV(port), nil
}

// --- regex ---

func bRegexMatch(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("regex_match", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString || args[1].Kind != VString {
		return Nil(), fmt.Errorf("regex_match wants (string, pattern) strings")
	}
	ok, err := regexp.MatchString(args[1].Str, args[0].Str)
	if err != nil {
		return Nil(), fmt.Errorf("regex_match bad pattern: %v", err)
	}
	return BoolV(ok), nil
}

func bRegexFind(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("regex_find", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString || args[1].Kind != VString {
		return Nil(), fmt.Errorf("regex_find wants (string, pattern) strings")
	}
	re, err := regexp.Compile(args[1].Str)
	if err != nil {
		return Nil(), fmt.Errorf("regex_find bad pattern: %v", err)
	}
	m := re.FindAllString(args[0].Str, -1)
	if m == nil {
		m = []string{}
	}
	out := make([]Value, 0, len(m))
	for _, s := range m {
		out = append(out, StrV(s))
	}
	return ArrV(out), nil
}

func bRegexReplace(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("regex_replace", args, 3, 3); err != nil {
		return Nil(), err
	}
	for _, a := range args {
		if a.Kind != VString {
			return Nil(), fmt.Errorf("regex_replace wants strings, got %s", TypeName(a))
		}
	}
	re, err := regexp.Compile(args[1].Str)
	if err != nil {
		return Nil(), fmt.Errorf("regex_replace bad pattern: %v", err)
	}
	return StrV(re.ReplaceAllString(args[0].Str, args[2].Str)), nil
}

func bRegexSplit(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("regex_split", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString || args[1].Kind != VString {
		return Nil(), fmt.Errorf("regex_split wants (string, pattern) strings")
	}
	re, err := regexp.Compile(args[1].Str)
	if err != nil {
		return Nil(), fmt.Errorf("regex_split bad pattern: %v", err)
	}
	parts := re.Split(args[0].Str, -1)
	out := make([]Value, 0, len(parts))
	for _, s := range parts {
		out = append(out, StrV(s))
	}
	return ArrV(out), nil
}

// --- crypto / encoding ---

func bSha256(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("sha256", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("sha256 wants string, got %s", TypeName(args[0]))
	}
	sum := sha256.Sum256([]byte(args[0].Str))
	return StrV(hex.EncodeToString(sum[:])), nil
}

func bMd5(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("md5", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("md5 wants string, got %s", TypeName(args[0]))
	}
	sum := md5.Sum([]byte(args[0].Str))
	return StrV(hex.EncodeToString(sum[:])), nil
}

func bHmacSha256(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("hmac_sha256", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString || args[1].Kind != VString {
		return Nil(), fmt.Errorf("hmac_sha256 wants (msg, key) strings")
	}
	m := hmac.New(sha256.New, []byte(args[1].Str))
	m.Write([]byte(args[0].Str))
	return StrV(hex.EncodeToString(m.Sum(nil))), nil
}

func bBase64Encode(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("base64_encode", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("base64_encode wants string, got %s", TypeName(args[0]))
	}
	return StrV(base64.StdEncoding.EncodeToString([]byte(args[0].Str))), nil
}

func bBase64Decode(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("base64_decode", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("base64_decode wants string, got %s", TypeName(args[0]))
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(args[0].Str))
	if err != nil {
		return Nil(), fmt.Errorf("base64_decode failed: %v", err)
	}
	return StrV(string(data)), nil
}

func bHexEncode(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("hex_encode", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("hex_encode wants string, got %s", TypeName(args[0]))
	}
	return StrV(hex.EncodeToString([]byte(args[0].Str))), nil
}

func bHexDecode(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("hex_decode", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("hex_decode wants string, got %s", TypeName(args[0]))
	}
	data, err := hex.DecodeString(strings.TrimSpace(args[0].Str))
	if err != nil {
		return Nil(), fmt.Errorf("hex_decode failed: %v", err)
	}
	return StrV(string(data)), nil
}

func bUUID(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("uuid", args, 0, 0); err != nil {
		return Nil(), err
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return Nil(), err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return StrV(fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])), nil
}

func bRandomBytes(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("random_bytes", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VInt || args[0].Int < 0 || args[0].Int > 65536 {
		return Nil(), fmt.Errorf("random_bytes wants int 0..65536")
	}
	buf := make([]byte, args[0].Int)
	if _, err := rand.Read(buf); err != nil {
		return Nil(), err
	}
	out := make([]Value, 0, len(buf))
	for _, b := range buf {
		out = append(out, IntV(int(b)))
	}
	return ArrV(out), nil
}

// --- fs full ---

func bStat(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("stat", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("stat wants path string, got %s", TypeName(args[0]))
	}
	fi, err := os.Stat(args[0].Str)
	if err != nil {
		return Nil(), err
	}
	return MapV(map[string]Value{
		"size":   IntV(int(fi.Size())),
		"is_dir": BoolV(fi.IsDir()),
		"mod":    IntV(int(fi.ModTime().UnixMilli())),
		"name":   StrV(fi.Name()),
	}), nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	fi, err := os.Stat(src)
	mode := os.FileMode(0o644)
	if err == nil {
		mode = fi.Mode()
	}
	return os.WriteFile(dst, data, mode)
}

func bCp(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("cp", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString || args[1].Kind != VString {
		return Nil(), fmt.Errorf("cp wants (src, dst) strings")
	}
	if err := copyFile(args[0].Str, args[1].Str); err != nil {
		return Nil(), err
	}
	return Nil(), nil
}

func bMv(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("mv", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString || args[1].Kind != VString {
		return Nil(), fmt.Errorf("mv wants (src, dst) strings")
	}
	if err := os.Rename(args[0].Str, args[1].Str); err != nil {
		// cross-device fallback
		if err2 := copyFile(args[0].Str, args[1].Str); err2 != nil {
			return Nil(), err
		}
		if err2 := os.Remove(args[0].Str); err2 != nil {
			return Nil(), err2
		}
	}
	return Nil(), nil
}

func bGlob(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("glob", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("glob wants pattern string, got %s", TypeName(args[0]))
	}
	m, err := filepath.Glob(args[0].Str)
	if err != nil {
		return Nil(), err
	}
	if m == nil {
		m = []string{}
	}
	sort.Strings(m)
	out := make([]Value, 0, len(m))
	for _, s := range m {
		out = append(out, StrV(s))
	}
	return ArrV(out), nil
}

func bPathJoin(in *Interpreter, args []Value) (Value, error) {
	if len(args) == 0 {
		return Nil(), fmt.Errorf("path_join wants at least 1 arg, got 0")
	}
	parts := make([]string, 0, len(args))
	for _, a := range args {
		if a.Kind != VString {
			return Nil(), fmt.Errorf("path_join wants strings, got %s", TypeName(a))
		}
		parts = append(parts, a.Str)
	}
	return StrV(filepath.Join(parts...)), nil
}

func bAbsPath(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("abs_path", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("abs_path wants string, got %s", TypeName(args[0]))
	}
	p, err := filepath.Abs(args[0].Str)
	if err != nil {
		return Nil(), err
	}
	return StrV(p), nil
}

func bRemove(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("remove", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("remove wants path string, got %s", TypeName(args[0]))
	}
	if err := os.RemoveAll(args[0].Str); err != nil {
		return Nil(), err
	}
	return Nil(), nil
}

// --- process ---

func bExec(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("exec", args, 1, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("exec wants cmd string, got %s", TypeName(args[0]))
	}
	var argv []string
	if len(args) == 2 {
		if args[1].Kind != VArray {
			return Nil(), fmt.Errorf("exec args must be array, got %s", TypeName(args[1]))
		}
		args[1].Arr.Mu.RLock()
		for _, e := range args[1].Arr.Items {
			if e.Kind != VString {
				args[1].Arr.Mu.RUnlock()
				return Nil(), fmt.Errorf("exec args must be strings")
			}
			argv = append(argv, e.Str)
		}
		args[1].Arr.Mu.RUnlock()
	}
	cmd := exec.Command(args[0].Str, argv...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			return Nil(), err
		}
	}
	return MapV(map[string]Value{
		"code":   IntV(code),
		"output": StrV(string(out)),
	}), nil
}

func bShell(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("shell", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("shell wants string, got %s", TypeName(args[0]))
	}
	cmd := exec.Command("sh", "-c", args[0].Str)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			return Nil(), err
		}
	}
	return MapV(map[string]Value{
		"code":   IntV(code),
		"output": StrV(string(out)),
	}), nil
}

func bCwd(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("cwd", args, 0, 0); err != nil {
		return Nil(), err
	}
	d, err := os.Getwd()
	if err != nil {
		return Nil(), err
	}
	return StrV(d), nil
}

func bEnvAll(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("env_all", args, 0, 0); err != nil {
		return Nil(), err
	}
	m := map[string]Value{}
	for _, kv := range os.Environ() {
		if i := strings.Index(kv, "="); i >= 0 {
			m[kv[:i]] = StrV(kv[i+1:])
		}
	}
	return MapV(m), nil
}

// --- time ---

func bFormatTime(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("format_time", args, 1, 2); err != nil {
		return Nil(), err
	}
	var ms int64
	switch args[0].Kind {
	case VInt:
		ms = int64(args[0].Int)
	case VFloat:
		ms = int64(args[0].Float)
	default:
		return Nil(), fmt.Errorf("format_time wants ms int, got %s", TypeName(args[0]))
	}
	layout := time.RFC3339
	if len(args) == 2 {
		if args[1].Kind != VString {
			return Nil(), fmt.Errorf("format_time layout must be string")
		}
		switch args[1].Str {
		case "date":
			layout = "2006-01-02"
		case "time":
			layout = "15:04:05"
		case "datetime", "local":
			layout = "2006-01-02 15:04:05"
		case "rfc3339":
			layout = time.RFC3339
		default:
			layout = args[1].Str
		}
	}
	return StrV(time.UnixMilli(ms).Format(layout)), nil
}

func bParseTime(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("parse_time", args, 1, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("parse_time wants string, got %s", TypeName(args[0]))
	}
	s := args[0].Str
	layouts := []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02", time.RFC1123, time.ANSIC}
	if len(args) == 2 {
		if args[1].Kind != VString {
			return Nil(), fmt.Errorf("parse_time layout must be string")
		}
		layouts = []string{args[1].Str}
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return IntV(int(t.UnixMilli())), nil
		}
	}
	return Nil(), fmt.Errorf("parse_time failed for %q", s)
}

func bTimeParts(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("time_parts", args, 1, 1); err != nil {
		return Nil(), err
	}
	var ms int64
	switch args[0].Kind {
	case VInt:
		ms = int64(args[0].Int)
	case VFloat:
		ms = int64(args[0].Float)
	default:
		return Nil(), fmt.Errorf("time_parts wants ms int, got %s", TypeName(args[0]))
	}
	t := time.UnixMilli(ms)
	return MapV(map[string]Value{
		"year":   IntV(t.Year()),
		"month":  IntV(int(t.Month())),
		"day":    IntV(t.Day()),
		"hour":   IntV(t.Hour()),
		"min":    IntV(t.Minute()),
		"sec":    IntV(t.Second()),
		"wday":   IntV(int(t.Weekday())),
		"unix":   IntV(int(t.Unix())),
		"millis": IntV(int(t.UnixMilli())),
	}), nil
}

// --- tiny JSON-file KV (sqlite_* first step) ---

func dbLoad(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func dbSave(path string, m map[string]any) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func bDbPut(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("db_put", args, 3, 3); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString || args[1].Kind != VString {
		return Nil(), fmt.Errorf("db_put wants (db_path, key, value)")
	}
	m, err := dbLoad(args[0].Str)
	if err != nil {
		return Nil(), err
	}
	j, err := valueToJSON(args[2])
	if err != nil {
		return Nil(), err
	}
	m[args[1].Str] = j
	if err := dbSave(args[0].Str, m); err != nil {
		return Nil(), err
	}
	return Nil(), nil
}

func bDbGet(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("db_get", args, 2, 3); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString || args[1].Kind != VString {
		return Nil(), fmt.Errorf("db_get wants (db_path, key)")
	}
	m, err := dbLoad(args[0].Str)
	if err != nil {
		return Nil(), err
	}
	v, ok := m[args[1].Str]
	if !ok {
		if len(args) == 3 {
			return args[2], nil
		}
		return Nil(), nil
	}
	return jsonToValue(v), nil
}

func bDbDelete(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("db_delete", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString || args[1].Kind != VString {
		return Nil(), fmt.Errorf("db_delete wants (db_path, key)")
	}
	m, err := dbLoad(args[0].Str)
	if err != nil {
		return Nil(), err
	}
	_, ok := m[args[1].Str]
	delete(m, args[1].Str)
	if err := dbSave(args[0].Str, m); err != nil {
		return Nil(), err
	}
	return BoolV(ok), nil
}

func bDbList(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("db_list", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("db_list wants db_path string")
	}
	m, err := dbLoad(args[0].Str)
	if err != nil {
		return Nil(), err
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Value, 0, len(keys))
	for _, k := range keys {
		out = append(out, StrV(k))
	}
	return ArrV(out), nil
}

// --- log / test helpers ---

func bLogInfo(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("log_info", args, 1, 1); err != nil {
		return Nil(), err
	}
	in.outMu.Lock()
	fmt.Fprintf(os.Stderr, "[info] %s\n", args[0].Display())
	in.outMu.Unlock()
	return Nil(), nil
}

func bLogWarn(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("log_warn", args, 1, 1); err != nil {
		return Nil(), err
	}
	in.outMu.Lock()
	fmt.Fprintf(os.Stderr, "[warn] %s\n", args[0].Display())
	in.outMu.Unlock()
	return Nil(), nil
}

func bLogError(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("log_error", args, 1, 1); err != nil {
		return Nil(), err
	}
	in.outMu.Lock()
	fmt.Fprintf(os.Stderr, "[error] %s\n", args[0].Display())
	in.outMu.Unlock()
	return Nil(), nil
}

func bAssertEq(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("assert_eq", args, 2, 3); err != nil {
		return Nil(), err
	}
	if !deepEqual(args[0], args[1]) {
		msg := fmt.Sprintf("assert_eq failed: %s != %s", args[0].Display(), args[1].Display())
		if len(args) == 3 {
			msg += ": " + args[2].Display()
		}
		return Nil(), fmt.Errorf("%s", msg)
	}
	return Nil(), nil
}

func bAssertNe(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("assert_ne", args, 2, 3); err != nil {
		return Nil(), err
	}
	if deepEqual(args[0], args[1]) {
		msg := fmt.Sprintf("assert_ne failed: both %s", args[0].Display())
		if len(args) == 3 {
			msg += ": " + args[2].Display()
		}
		return Nil(), fmt.Errorf("%s", msg)
	}
	return Nil(), nil
}

func bAssertContains(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("assert_contains", args, 2, 3); err != nil {
		return Nil(), err
	}
	found := false
	switch {
	case args[0].Kind == VString && args[1].Kind == VString:
		found = strings.Contains(args[0].Str, args[1].Str)
	case args[0].Kind == VArray:
		args[0].Arr.Mu.RLock()
		for _, e := range args[0].Arr.Items {
			if deepEqual(e, args[1]) {
				found = true
				break
			}
		}
		args[0].Arr.Mu.RUnlock()
	case args[0].Kind == VMap && args[1].Kind == VString:
		args[0].Map.Mu.RLock()
		_, found = args[0].Map.Vals[args[1].Str]
		args[0].Map.Mu.RUnlock()
	default:
		return Nil(), fmt.Errorf("assert_contains wants (string/array/map, value)")
	}
	if !found {
		msg := fmt.Sprintf("assert_contains failed: %s missing %s", args[0].Display(), args[1].Display())
		if len(args) == 3 {
			msg += ": " + args[2].Display()
		}
		return Nil(), fmt.Errorf("%s", msg)
	}
	return Nil(), nil
}

// --- concurrency helpers ---

func bWithTimeout(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("with_timeout", args, 2, 2); err != nil {
		return Nil(), err
	}
	ms, err := toMillis(args[0])
	if err != nil {
		return Nil(), fmt.Errorf("with_timeout ms: %v", err)
	}
	if args[1].Kind != VFunc && args[1].Kind != VBuiltin {
		return Nil(), fmt.Errorf("with_timeout wants func, got %s", TypeName(args[1]))
	}
	fn := args[1]
	done := make(chan Value, 1)
	errCh := make(chan error, 1)
	go func() {
		v, err := in.callValue(fn, []Value{})
		if err != nil {
			errCh <- err
			return
		}
		done <- v
	}()
	select {
	case v := <-done:
		return v, nil
	case err := <-errCh:
		return Nil(), err
	case <-time.After(time.Duration(ms) * time.Millisecond):
		return Nil(), fmt.Errorf("with_timeout: timed out after %dms", ms)
	}
}

func bParallel(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("parallel", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VArray {
		return Nil(), fmt.Errorf("parallel wants array, got %s", TypeName(args[0]))
	}
	if args[1].Kind != VFunc && args[1].Kind != VBuiltin {
		return Nil(), fmt.Errorf("parallel wants func, got %s", TypeName(args[1]))
	}
	args[0].Arr.Mu.RLock()
	items := make([]Value, len(args[0].Arr.Items))
	copy(items, args[0].Arr.Items)
	args[0].Arr.Mu.RUnlock()
	fn := args[1]
	out := make([]Value, len(items))
	errs := make([]error, len(items))
	done := make(chan int, len(items))
	for i, e := range items {
		go func(idx int, val Value) {
			v, err := in.callValue(fn, []Value{val})
			if err != nil {
				errs[idx] = err
			} else {
				out[idx] = v
			}
			done <- idx
		}(i, e)
	}
	for range items {
		<-done
	}
	for _, e := range errs {
		if e != nil {
			return Nil(), e
		}
	}
	return ArrV(out), nil
}

// --- types: struct / enum ---

func bStructValidate(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("struct_validate", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VMap || args[1].Kind != VMap {
		return Nil(), fmt.Errorf("struct_validate wants (value: map, schema: map)")
	}
	args[0].Map.Mu.RLock()
	vals := make(map[string]Value, len(args[0].Map.Vals))
	for k, v := range args[0].Map.Vals {
		vals[k] = v
	}
	args[0].Map.Mu.RUnlock()
	args[1].Map.Mu.RLock()
	defer args[1].Map.Mu.RUnlock()
	for field, typVal := range args[1].Map.Vals {
		if typVal.Kind != VString {
			return Nil(), fmt.Errorf("struct_validate schema field %q must be type string", field)
		}
		v, ok := vals[field]
		if !ok {
			return BoolV(false), nil
		}
		if !matchesTypeNullable(v, typVal.Str) {
			return BoolV(false), nil
		}
	}
	return BoolV(true), nil
}

func bStructAssert(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("struct_assert", args, 2, 2); err != nil {
		return Nil(), err
	}
	ok, err := bStructValidate(in, args)
	if err != nil {
		return Nil(), err
	}
	if ok.Kind == VBool && ok.Bool {
		return args[0], nil
	}
	return Nil(), fmt.Errorf("struct_assert failed")
}

func bEnumCreate(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("enum_create", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VArray {
		return Nil(), fmt.Errorf("enum_create wants array of strings, got %s", TypeName(args[0]))
	}
	args[0].Arr.Mu.RLock()
	defer args[0].Arr.Mu.RUnlock()
	m := map[string]Value{}
	for i, e := range args[0].Arr.Items {
		if e.Kind != VString {
			return Nil(), fmt.Errorf("enum_create wants strings, got %s", TypeName(e))
		}
		m[e.Str] = IntV(i)
	}
	return MapV(m), nil
}

func bEnumValid(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("enum_valid", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VMap || args[1].Kind != VString {
		return Nil(), fmt.Errorf("enum_valid wants (enum: map, value: string)")
	}
	args[0].Map.Mu.RLock()
	defer args[0].Map.Mu.RUnlock()
	_, ok := args[0].Map.Vals[args[1].Str]
	return BoolV(ok), nil
}

func bIsNumber(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("is_number", args, 1, 1); err != nil {
		return Nil(), err
	}
	return BoolV(isNum(args[0])), nil
}

func bTrimPrefix(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("trim_prefix", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString || args[1].Kind != VString {
		return Nil(), fmt.Errorf("trim_prefix wants strings")
	}
	return StrV(strings.TrimPrefix(args[0].Str, args[1].Str)), nil
}

func bTrimSuffix(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("trim_suffix", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString || args[1].Kind != VString {
		return Nil(), fmt.Errorf("trim_suffix wants strings")
	}
	return StrV(strings.TrimSuffix(args[0].Str, args[1].Str)), nil
}

func bRepeatStr(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("repeat_str", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString || args[1].Kind != VInt {
		return Nil(), fmt.Errorf("repeat_str wants (string, int)")
	}
	if args[1].Int < 0 || args[1].Int > 100000 {
		return Nil(), fmt.Errorf("repeat_str count out of range")
	}
	return StrV(strings.Repeat(args[0].Str, args[1].Int)), nil
}
