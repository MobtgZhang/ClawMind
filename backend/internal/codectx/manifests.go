package codectx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// inferCommands derives test/build/lint/format commands from manifests present in the workspace.
func inferCommands(root string, manifests []string) Commands {
	has := map[string]struct{}{}
	for _, m := range manifests {
		has[filepath.Base(m)] = struct{}{}
	}
	lookup := func(name string) bool {
		_, ok := has[name]
		return ok
	}
	var c Commands
	switch {
	case lookup("go.mod"):
		c.Test = "go test ./..."
		c.Build = "go build ./..."
		c.Format = "gofmt -l ."
	case lookup("Cargo.toml"):
		c.Test = "cargo test"
		c.Build = "cargo build"
		c.Lint = "cargo clippy"
	case lookup("pyproject.toml") || lookup("pytest.ini") || lookup("setup.cfg") || lookup("requirements.txt"):
		c.Test = "pytest -q"
		c.Lint = "ruff check ."
		c.Format = "ruff format ."
	}
	if lookup("package.json") {
		if scripts := npmScripts(filepath.Join(root, "package.json")); len(scripts) > 0 {
			if v := scripts["test"]; v != "" {
				c.Test = v
			} else if c.Test == "" {
				c.Test = "npm test"
			}
			if v := scripts["build"]; v != "" {
				c.Build = v
			}
			if v := scripts["lint"]; v != "" {
				c.Lint = v
			}
		}
	}
	if lookup("Makefile") && c.Test == "" {
		for _, want := range []string{"test", "check", "vet"} {
			if _, ok := makeTargets(filepath.Join(root, "Makefile"))[want]; ok {
				c.Test = "make " + want
				break
			}
		}
	}
	if lookup("pom.xml") && c.Test == "" {
		c.Test = "mvn -q test"
	}
	if (lookup("build.gradle") || lookup("build.gradle.kts")) && c.Test == "" {
		c.Test = "./gradlew test"
	}
	return c
}

// npmScripts reads package.json "scripts" (best effort; nil on parse failure).
func npmScripts(path string) map[string]string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		return nil
	}
	return pkg.Scripts
}

// makeTargets reads target names from a Makefile.
func makeTargets(path string) map[string]struct{} {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	out := map[string]struct{}{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ".PHONY") {
			continue
		}
		if !strings.HasSuffix(line, ":") {
			continue
		}
		name := strings.TrimSpace(strings.TrimSuffix(line, ":"))
		if name == "" || strings.ContainsAny(name, " \t") {
			continue
		}
		out[name] = struct{}{}
	}
	return out
}

// HasManifest reports whether a manifest file exists at the workspace root.
func HasManifest(root, name string) bool {
	_, err := os.Stat(filepath.Join(root, name))
	return err == nil
}
