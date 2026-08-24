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
// Scoped to the Go side. The SPA has no test runner; its equivalent single
// source is web/src/lib/brand.ts.
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
