package architecture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

var featurePackages = []string{
	"./internal/organization",
	"./internal/user",
	"./internal/monitor",
	"./internal/check",
}

type listedPackage struct {
	ImportPath string
	Standard   bool
	DepOnly    bool
}

func TestFeaturePackagesUseOnlyTheStandardLibrary(t *testing.T) {
	moduleRoot := findModuleRoot(t)
	goTool := findGoTool(t)

	for _, featurePackage := range featurePackages {
		featurePackage := featurePackage
		t.Run(filepath.Base(featurePackage), func(t *testing.T) {
			packages := listDependencies(t, goTool, moduleRoot, featurePackage)
			rootPackage := listedRootPackage(t, packages)
			modulePath := modulePathFor(t, rootPackage.ImportPath, featurePackage)

			var violations []string
			for _, dependency := range packages {
				if dependency.ImportPath == rootPackage.ImportPath {
					continue
				}

				switch {
				case isForbiddenAdapter(modulePath, dependency.ImportPath):
					violations = append(violations, fmt.Sprintf("%s (forbidden adapter)", dependency.ImportPath))
				case !dependency.Standard:
					violations = append(violations, fmt.Sprintf("%s (non-standard-library package)", dependency.ImportPath))
				}
			}

			sort.Strings(violations)
			if len(violations) != 0 {
				t.Fatalf("%s has forbidden direct or transitive dependencies:\n  %s", rootPackage.ImportPath, strings.Join(violations, "\n  "))
			}
		})
	}
}

func listDependencies(t *testing.T, goTool, moduleRoot, featurePackage string) []listedPackage {
	t.Helper()

	command := exec.Command(goTool, "list", "-mod=readonly", "-deps", "-json", featurePackage)
	command.Dir = moduleRoot
	command.Env = withEnvironment(os.Environ(), "GOWORK", "off")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("go list failed for %s: %v\n%s", featurePackage, err, strings.TrimSpace(stderr.String()))
	}

	decoder := json.NewDecoder(&stdout)
	var packages []listedPackage
	for {
		var current listedPackage
		err := decoder.Decode(&current)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode go list output for %s: %v", featurePackage, err)
		}
		packages = append(packages, current)
	}
	return packages
}

func listedRootPackage(t *testing.T, packages []listedPackage) listedPackage {
	t.Helper()

	var roots []listedPackage
	for _, current := range packages {
		if !current.DepOnly {
			roots = append(roots, current)
		}
	}
	if len(roots) != 1 {
		t.Fatalf("go list returned %d root packages, want 1", len(roots))
	}
	return roots[0]
}

func modulePathFor(t *testing.T, importPath, featurePackage string) string {
	t.Helper()

	suffix := strings.TrimPrefix(featurePackage, ".")
	if !strings.HasSuffix(importPath, suffix) {
		t.Fatalf("root import path %q does not end in %q", importPath, suffix)
	}
	return strings.TrimSuffix(importPath, suffix)
}

func isForbiddenAdapter(modulePath, importPath string) bool {
	for _, adapter := range []string{"internal/postgres", "internal/httpapi"} {
		prefix := modulePath + "/" + adapter
		if importPath == prefix || strings.HasPrefix(importPath, prefix+"/") {
			return true
		}
	}
	return false
}

func findModuleRoot(t *testing.T) string {
	t.Helper()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate architecture test source")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
	if _, err := os.Stat(filepath.Join(moduleRoot, "go.mod")); err != nil {
		t.Fatalf("locate module root from %s: %v", sourceFile, err)
	}
	return moduleRoot
}

func findGoTool(t *testing.T) string {
	t.Helper()

	name := "go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	goTool := filepath.Join(runtime.GOROOT(), "bin", name)
	if _, err := os.Stat(goTool); err != nil {
		t.Fatalf("locate Go tool at %s: %v", goTool, err)
	}
	return goTool
}

func withEnvironment(environment []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}
