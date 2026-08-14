package entitlement

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

func testRoll() *RollProvider {
	return NewRollProvider([]string{"Willow Lane", "Mill St"}, 24*time.Hour)
}

// The point of the provider: the applicant supplies an address, the provider
// decides. An address off the roll is refused — which the old endpoint could
// not do, because it simply set the flag from whatever was posted.
func TestRollDecidesRatherThanAccepts(t *testing.T) {
	p := testRoll()
	cases := []struct {
		name    string
		address string
		want    domain.EntitlementOutcome
	}{
		{"on the roll", "12 Willow Lane", domain.EntitlementGranted},
		{"on the roll, different number", "9999 Mill St", domain.EntitlementGranted},
		{"case and spacing are not a way in", "  12   wILLow   lane ", domain.EntitlementGranted},
		{"off the roll", "1 Elsewhere Blvd", domain.EntitlementDenied},
		{"plausible but not on the roll", "12 Willow Street", domain.EntitlementDenied},
		{"empty street", "12", domain.EntitlementDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := p.Enrol(context.Background(), TypeResidency, map[string]string{"address": tc.address})
			if err != nil {
				t.Fatal(err)
			}
			if res.Outcome != tc.want {
				t.Fatalf("%q → %s, want %s", tc.address, res.Outcome, tc.want)
			}
		})
	}
}

func TestRollRejectsEmptyInput(t *testing.T) {
	p := testRoll()
	if _, err := p.Enrol(context.Background(), TypeResidency, map[string]string{"address": "   "}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("want ErrInvalidInput, got %v", err)
	}
}

// A granted determination expires: people move, so residency is re-checked.
func TestRollGrantsWithExpiry(t *testing.T) {
	p := testRoll()
	res, err := p.Enrol(context.Background(), TypeResidency, map[string]string{"address": "12 Willow Lane"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ValidUntil == nil {
		t.Fatal("a granted determination must carry an expiry")
	}
	if res.Ref == "" {
		t.Fatal("a granted determination must carry a reference for silent re-validation")
	}
}

// The reference must not be the address itself: it lives in our database, and
// the evidence is meant to stay with the provider.
func TestRefDoesNotContainTheAddress(t *testing.T) {
	p := testRoll()
	res, _ := p.Enrol(context.Background(), TypeResidency, map[string]string{"address": "12 Willow Lane"})
	for _, leak := range []string{"willow", "Willow", "12"} {
		if len(res.Ref) > 0 && contains(res.Ref, leak) {
			t.Errorf("reference %q leaks the submitted address (%q)", res.Ref, leak)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// Re-validation resolves the stored reference back to a decision, so a
// returning resident proves nothing again.
func TestRollEvaluateRoundTrips(t *testing.T) {
	p := testRoll()
	res, _ := p.Enrol(context.Background(), TypeResidency, map[string]string{"address": "12 Willow Lane"})

	again, err := p.Evaluate(context.Background(), TypeResidency, res.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if again.Outcome != domain.EntitlementGranted {
		t.Fatalf("re-validation = %s, want granted", again.Outcome)
	}
}

// A street struck from the roll stops qualifying at the next check, and the
// stale reference is reported as unknown rather than silently granted.
func TestRollEvaluateUnknownRef(t *testing.T) {
	p := testRoll()
	res, _ := p.Enrol(context.Background(), TypeResidency, map[string]string{"address": "12 Willow Lane"})

	shrunk := NewRollProvider([]string{"Mill St"}, 24*time.Hour)
	if _, err := shrunk.Evaluate(context.Background(), TypeResidency, res.Ref); !errors.Is(err, ErrRefUnknown) {
		t.Fatalf("want ErrRefUnknown once the street leaves the roll, got %v", err)
	}
}

// The published contract tells the applicant what to supply — not what makes
// the check pass.
func TestDescriptorPublishesInputsNotCriteria(t *testing.T) {
	p := testRoll()
	d := p.Describe(TypeResidency)
	if len(d.Fields) == 0 {
		t.Fatal("the descriptor must publish the inputs the form needs")
	}
	if d.Statement == "" {
		t.Error("a human-readable policy statement is expected")
	}
	// The roll is the passing criteria; it must not be in the descriptor.
	for _, street := range []string{"Willow Lane", "Mill St"} {
		if contains(d.Statement, street) {
			t.Errorf("descriptor leaks the address roll (%q) — that tells someone what to forge", street)
		}
	}
}
