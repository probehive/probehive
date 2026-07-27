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

// standardLibraryOnlyPackages are the packages ADR 0002 and ADR 0020 hold to the standard
// library alone: the feature packages, the check catalog, and the outbound policy engine.
// internal/outbound is on the list because its whole value is being small enough to review,
// and because a policy engine that can reach for an HTTP client is one refactor away from
// deciding what it is enforcing against.
var standardLibraryOnlyPackages = []string{
	"./internal/organization",
	"./internal/user",
	"./internal/monitor",
	"./internal/run",
	"./internal/check",
	"./internal/outbound",
}

// adapterFreePackages are packages ADR 0020 bars from persistence and composition without
// holding them to the standard library. internal/probe speaks protocols for a living, so an
// HTTP client is its purpose rather than a violation; what it must never learn is where an
// Observation is stored or who composed it.
var adapterFreePackages = []string{
	"./internal/probe",
}

// forbiddenStandardPackages are standard-library packages ADR 0002 names explicitly:
// feature packages "import no SQL package, HTTP package, database driver, composition
// package, or sibling feature implementation". Membership of the standard library is not
// a licence to reach for transport or persistence, and check execution will be tempted to
// import net/http into internal/check, so the rule is enforced rather than trusted.
var forbiddenStandardPackages = []string{
	"net/http",
	"net/rpc",
	"database/sql",
}

// isForbiddenStandard matches a package and everything beneath it, so net/http/httptest
// is caught along with net/http while net/url and net/netip stay allowed.
func isForbiddenStandard(importPath string) bool {
	for _, forbidden := range forbiddenStandardPackages {
		if importPath == forbidden || strings.HasPrefix(importPath, forbidden+"/") {
			return true
		}
	}
	return false
}

type listedPackage struct {
	ImportPath string
	Standard   bool
	DepOnly    bool
}

func TestDeclaredPackagesUseOnlyTheStandardLibrary(t *testing.T) {
	moduleRoot := findModuleRoot(t)
	goTool := findGoTool(t)

	for _, declaredPackage := range standardLibraryOnlyPackages {
		declaredPackage := declaredPackage
		t.Run(filepath.Base(declaredPackage), func(t *testing.T) {
			packages := listDependencies(t, goTool, moduleRoot, declaredPackage)
			rootPackage := listedRootPackage(t, packages)
			modulePath := modulePathFor(t, rootPackage.ImportPath, declaredPackage)

			var violations []string
			for _, dependency := range packages {
				if dependency.ImportPath == rootPackage.ImportPath {
					continue
				}

				switch {
				case isForbiddenAdapter(modulePath, dependency.ImportPath):
					violations = append(violations, fmt.Sprintf("%s (forbidden adapter)", dependency.ImportPath))
				case isForbiddenStandard(dependency.ImportPath):
					violations = append(violations, fmt.Sprintf("%s (transport or persistence package)", dependency.ImportPath))
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

func TestDeclaredPackagesAvoidPersistenceAndComposition(t *testing.T) {
	moduleRoot := findModuleRoot(t)
	goTool := findGoTool(t)

	for _, declaredPackage := range adapterFreePackages {
		declaredPackage := declaredPackage
		t.Run(filepath.Base(declaredPackage), func(t *testing.T) {
			packages := listDependencies(t, goTool, moduleRoot, declaredPackage)
			rootPackage := listedRootPackage(t, packages)
			modulePath := modulePathFor(t, rootPackage.ImportPath, declaredPackage)

			var violations []string
			for _, dependency := range packages {
				if dependency.ImportPath != rootPackage.ImportPath && isForbiddenAdapter(modulePath, dependency.ImportPath) {
					violations = append(violations, dependency.ImportPath)
				}
			}

			sort.Strings(violations)
			if len(violations) != 0 {
				t.Fatalf("%s has forbidden direct or transitive dependencies:\n  %s", rootPackage.ImportPath, strings.Join(violations, "\n  "))
			}
		})
	}
}

func listDependencies(t *testing.T, goTool, moduleRoot, declaredPackage string) []listedPackage {
	t.Helper()

	command := exec.Command(goTool, "list", "-mod=readonly", "-deps", "-json", declaredPackage)
	command.Dir = moduleRoot
	command.Env = withEnvironment(os.Environ(), "GOWORK", "off")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("go list failed for %s: %v\n%s", declaredPackage, err, strings.TrimSpace(stderr.String()))
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
			t.Fatalf("decode go list output for %s: %v", declaredPackage, err)
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

func modulePathFor(t *testing.T, importPath, declaredPackage string) string {
	t.Helper()

	suffix := strings.TrimPrefix(declaredPackage, ".")
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

// TestForbiddenStandardPackageClassification pins the classifier itself. Without it the
// guard above could silently match nothing and every dependency scan would still pass.
func TestForbiddenStandardPackageClassification(t *testing.T) {
	t.Parallel()
	forbidden := []string{
		"net/http", "net/http/httptest", "net/http/cookiejar", "database/sql", "database/sql/driver",
	}
	allowed := []string{"net", "net/url", "net/netip", "net/textproto", "encoding/json", "strings", "databasex"}

	for _, importPath := range forbidden {
		if !isForbiddenStandard(importPath) {
			t.Errorf("isForbiddenStandard(%q) = false, want true", importPath)
		}
	}
	for _, importPath := range allowed {
		if isForbiddenStandard(importPath) {
			t.Errorf("isForbiddenStandard(%q) = true, want false", importPath)
		}
	}
}
