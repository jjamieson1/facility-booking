package brand

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// FAC-20's actual promise is that rebranding is a config change, not a hunt
// through components. A promise nothing enforces decays the first time someone
// types the service name into a template, so this fails the build if the demo
// identity reappears in code outside the one file that owns it.
//
// Writing this test is what found the leaks it now guards: the waiver template
// and the C2 service card both had the name hardcoded in resident-facing text.
//
// The SPA is covered by TestDemoBrandDoesNotLeakIntoTheSPA below, for the
// reason recorded there.
func TestDemoBrandLivesInExactlyOnePlace(t *testing.T) {
	// Files that may legitimately name the demo municipality.
	exempt := func(path string) bool {
		switch {
		case strings.HasSuffix(path, "_test.go"):
			return true // tests assert on the demo data
		case strings.Contains(path, "/seed/"):
			return true // the seed IS the demo data
		case filepath.Base(path) == "brand.go":
			return true // the one place that owns it
		case strings.HasSuffix(path, "/config/config.go"):
			// Contact details are municipality branding too, but they are
			// already env-overridable (FB_CONTACT_*) with demo defaults, which is
			// the same contract this test exists to enforce.
			return true
		}
		return false
	}

	var offenders []string
	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || exempt(path) {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(body), "\n") {
			// Comments may name the demo data when explaining it; only code that
			// would reach a resident matters here.
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if strings.Contains(line, demoName) || strings.Contains(line, shortDemoName) {
				offenders = append(offenders, path+":"+strconv.Itoa(i+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(offenders) > 0 {
		t.Fatalf("the demo brand name appears outside internal/brand — rebranding is meant to be a config change, so route these through brand.Name()/brand.Short():\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

// The SPA leaked, exactly where this test's Go-side sibling said it could not
// reach: eight strings in the i18n bundles named the municipality directly
// ("Browse Rivermont's facilities", "Sign in with your Rivermont account"), in
// both languages and all resident-facing. Changing brand.ts would have left
// every one of them in place — the precise failure FAC-20 exists to prevent.
//
// This is a Go test walking TypeScript, which is odd, and deliberate. There is
// no CI here: deploy/deploy.sh gates on go build, go vet and go test, and the
// SPA has no test runner at all, so a vitest suite would enforce nothing until
// someone remembered to run it. A Go test is the only thing in this repository
// that actually blocks a leak from shipping. If the SPA gains a test runner and
// the deploy gate learns to run it, move this there.
func TestDemoBrandDoesNotLeakIntoTheSPA(t *testing.T) {
	const spa = "../../web/src"
	if _, err := os.Stat(spa); os.IsNotExist(err) {
		t.Skipf("no SPA sources at %s", spa)
	}

	// brand.ts owns the identity; i18n.ts may name the {{city}} VARIABLE but is
	// checked for the literal like everything else.
	exempt := func(path string) bool {
		return filepath.Base(path) == "brand.ts"
	}

	var offenders []string
	err := filepath.Walk(spa, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		ext := filepath.Ext(path)
		if info.IsDir() || (ext != ".ts" && ext != ".tsx") || exempt(path) {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(body), "\n") {
			trimmed := strings.TrimSpace(line)
			// Comments may name the demo identity while explaining it.
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
				continue
			}
			if strings.Contains(line, demoName) || strings.Contains(line, shortDemoName) {
				offenders = append(offenders, path+":"+strconv.Itoa(i+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(offenders) > 0 {
		t.Fatalf("the demo brand name appears in the SPA outside web/src/lib/brand.ts.\nUse the {{city}} interpolation variable (i18n.ts supplies it from brand.short) or import brand directly:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}
