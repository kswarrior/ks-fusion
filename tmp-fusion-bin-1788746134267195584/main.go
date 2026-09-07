package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kswarrior/ks-fusion/internal/backend"
	"github.com/kswarrior/ks-fusion/internal/frontend"
)

var embedded = map[string]string{
"backend/main.ks": "print \"e2e-backend-ok\"\n",
"frontend/main.ks": "print \"e2e-frontend-ok\"\n",
}

var fusionToml = "[package]\nname = \"e2e-bin\"\nversion = \"0.1.0\"\nentry_backend = \"backend/main.ks\"\nentry_frontend = \"frontend/main.ks\"\n"

func main() {
	dir, err := os.MkdirTemp("", "e2e-bin-bin-*")
	if err != nil { fmt.Println("error:", err); os.Exit(1) }
	// no cleanup: keep running while server lives; remove on exit
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "fusion.toml"), []byte(fusionToml), 0644); err != nil { fmt.Println("error:", err); os.Exit(1) }
	for rel, data := range embedded {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil { fmt.Println("error:", err); os.Exit(1) }
		if err := os.WriteFile(p, []byte(data), 0644); err != nil { fmt.Println("error:", err); os.Exit(1) }
	}
	_ = fusionToml
	for _, entry := range []string{"backend/main.ks", "frontend/main.ks"} {
		p := filepath.Join(dir, filepath.FromSlash(entry))
		prog, err := frontend.ParseFile(p)
		if err != nil { fmt.Println("error:", err); os.Exit(1) }
		if err := backend.RunWithDir(prog, dir); err != nil { fmt.Println("error:", err); os.Exit(1) }
	}
}
