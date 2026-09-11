package tix

import (
	"reflect"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestDerivePrefixesIsDeterministicAndUnique(t *testing.T) {
	names := []string{"zen", "acme", "frontend"}

	first := derivePrefixes(names, nil)
	want := map[string]string{"acme": "ac", "frontend": "fr", "zen": "ze"}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("derivePrefixes = %#v, want %#v", first, want)
	}

	// Input order must not change the assignment, or two processes reading the
	// same database would disagree about what "fr-47" means.
	second := derivePrefixes([]string{"frontend", "zen", "acme"}, nil)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("derivePrefixes is order-dependent: %#v vs %#v", first, second)
	}
}

func TestDerivePrefixesLengthensOnCollision(t *testing.T) {
	got := derivePrefixes([]string{"frontend", "fresh", "fred"}, nil)

	if len(got) != 3 {
		t.Fatalf("got %d prefixes, want 3", len(got))
	}
	seen := map[string]string{}
	for project, prefix := range got {
		if other, clash := seen[prefix]; clash {
			t.Fatalf("projects %q and %q share prefix %q", project, other, prefix)
		}
		seen[prefix] = project
		if !strings.HasPrefix(project, prefix) {
			t.Errorf("prefix %q is not drawn from project %q", prefix, project)
		}
	}
}

func TestDerivePrefixesHandlesExhaustedNames(t *testing.T) {
	// Both names reduce to the base "fe", which is only two characters long, so
	// the loser cannot lengthen its way out and falls back to a numeric suffix.
	// Sorted order decides the winner: "f-e" sorts before "fe".
	got := derivePrefixes([]string{"fe", "f-e"}, nil)

	if got["fe"] == got["f-e"] {
		t.Fatalf("both projects got prefix %q", got["fe"])
	}
	if got["f-e"] != "fe" {
		t.Errorf("got[f-e] = %q, want fe", got["f-e"])
	}
	if got["fe"] != "fe2" {
		t.Errorf("got[fe] = %q, want fe2", got["fe"])
	}
}

func TestDerivePrefixesFallsBackForNonAlphanumericNames(t *testing.T) {
	if got := prefixBase("!!!"); got != fallbackPrefix {
		t.Errorf("prefixBase(!!!) = %q, want %q", got, fallbackPrefix)
	}
	if got := prefixBase("Front-End!"); got != "frontend" {
		t.Errorf("prefixBase(Front-End!) = %q, want frontend", got)
	}
}

func TestOverrideWinsAndReservesPrefix(t *testing.T) {
	// An override takes the prefix a derived name would otherwise claim, so
	// the derived project must move out of the way.
	got := derivePrefixes([]string{"acme", "across"}, map[string]string{"across": "ac"})

	if got["across"] != "ac" {
		t.Errorf("override not honored: got[across] = %q, want ac", got["across"])
	}
	if got["acme"] == "ac" {
		t.Error("derived prefix collided with an override")
	}
}

func TestParseRefAcceptsBothForms(t *testing.T) {
	cases := []struct {
		ref    string
		id     int
		prefix string
	}{
		{"47", 47, ""},
		{"fe-47", 47, "fe"},
		{"#47", 47, ""},
		{" 47 ", 47, ""},
		{"fe2-47", 47, "fe2"},
	}
	for _, c := range cases {
		id, prefix, err := ParseRef(c.ref)
		if err != nil {
			t.Errorf("ParseRef(%q): %v", c.ref, err)
			continue
		}
		if id != c.id || prefix != c.prefix {
			t.Errorf("ParseRef(%q) = (%d, %q), want (%d, %q)", c.ref, id, prefix, c.id, c.prefix)
		}
	}

	for _, ref := range []string{"", "abc", "-47", "fe-", "0", "-1", "fe-abc"} {
		if _, _, err := ParseRef(ref); err == nil {
			t.Errorf("ParseRef(%q) succeeded, want error", ref)
		}
	}
}

func TestResolveRefRejectsWrongProjectPrefix(t *testing.T) {
	setupTestDB(t)

	acmeID, err := Add("fix login bug", "", 2, "acme")
	if err != nil {
		t.Fatalf("Add acme ticket: %v", err)
	}

	// The bare id still works, which is what keeps existing shell history and
	// scripts valid.
	if got, err := ResolveRef("1"); err != nil || got != int(acmeID) {
		t.Fatalf("ResolveRef(1) = (%d, %v), want (%d, nil)", got, err, acmeID)
	}

	// The right prefix resolves.
	if got, err := ResolveRef("ac-1"); err != nil || got != int(acmeID) {
		t.Fatalf("ResolveRef(ac-1) = (%d, %v), want (%d, nil)", got, err, acmeID)
	}

	// A prefix naming a different project is the case prefixes exist to catch.
	if _, err := ResolveRef("ze-1"); err == nil {
		t.Fatal("ResolveRef(ze-1) succeeded, want a project mismatch error")
	} else if !strings.Contains(err.Error(), "acme") {
		t.Errorf("error %q does not name the actual project", err)
	}

	if _, err := ResolveRef("ac-99"); err == nil {
		t.Error("ResolveRef(ac-99) succeeded, want no-such-ticket error")
	}
}

func TestSetPrefixOverridesAndRejectsDuplicates(t *testing.T) {
	setupTestDB(t)

	if _, err := Add("fix login bug", "", 2, "acme"); err != nil {
		t.Fatalf("Add acme ticket: %v", err)
	}
	if _, err := Add("refactor parser", "", 2, "zen"); err != nil {
		t.Fatalf("Add zen ticket: %v", err)
	}

	if err := SetPrefix("acme", "acm"); err != nil {
		t.Fatalf("SetPrefix(acme, acm): %v", err)
	}
	if got, err := PrefixFor("acme"); err != nil || got != "acm" {
		t.Fatalf("PrefixFor(acme) = (%q, %v), want (acm, nil)", got, err)
	}
	if _, err := ResolveRef("acm-1"); err != nil {
		t.Errorf("ResolveRef(acm-1) after override: %v", err)
	}

	// Re-pinning the same project is an update, not a conflict.
	if err := SetPrefix("acme", "aa"); err != nil {
		t.Fatalf("SetPrefix(acme, aa): %v", err)
	}

	if err := SetPrefix("zen", "aa"); err == nil {
		t.Error("SetPrefix reused another project's prefix, want error")
	}
	for _, bad := range []string{"", "a b", "fe-1"} {
		if err := SetPrefix("zen", bad); err == nil {
			t.Errorf("SetPrefix(zen, %q) succeeded, want error", bad)
		}
	}
}

func TestFormatID(t *testing.T) {
	if got := FormatID("fe", 47); got != "fe-47" {
		t.Errorf("FormatID(fe, 47) = %q, want fe-47", got)
	}
	if got := FormatID("", 47); got != "47" {
		t.Errorf("FormatID(\"\", 47) = %q, want 47", got)
	}
}
