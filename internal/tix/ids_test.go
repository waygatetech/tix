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
	newProjects(t, "acme")

	acmeID, err := Add("fix login bug", "", 2, "acme", nil)
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
	newProjects(t, "acme", "zen")

	if _, err := Add("fix login bug", "", 2, "acme", nil); err != nil {
		t.Fatalf("Add acme ticket: %v", err)
	}
	if _, err := Add("refactor parser", "", 2, "zen", nil); err != nil {
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

func TestShortIDs(t *testing.T) {
	tests := []struct {
		name string
		ids  []string
		want map[string]string
	}{
		{
			name: "single id uses the minimum",
			ids:  []string{"a3f9b2c1-0000-4000-8000-000000000000"},
			want: map[string]string{"a3f9b2c1-0000-4000-8000-000000000000": "a3f9"},
		},
		{
			name: "shared prefix lengthens only the rivals",
			ids: []string{
				"a3f90000-0000-4000-8000-000000000000",
				"a3f91111-0000-4000-8000-000000000000",
				"4747aaaa-0000-4000-8000-000000000000",
			},
			want: map[string]string{
				"a3f90000-0000-4000-8000-000000000000": "a3f90",
				"a3f91111-0000-4000-8000-000000000000": "a3f91",
				"4747aaaa-0000-4000-8000-000000000000": "4747",
			},
		},
		{
			name: "prefix past the first hyphen ignores it",
			ids: []string{
				"a3f9b2c1-0000-4000-8000-000000000000",
				"a3f9b2c1-1000-4000-8000-000000000000",
			},
			want: map[string]string{
				"a3f9b2c1-0000-4000-8000-000000000000": "a3f9b2c10",
				"a3f9b2c1-1000-4000-8000-000000000000": "a3f9b2c11",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShortIDs(tt.ids); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ShortIDs = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveUUIDRef(t *testing.T) {
	setupTestDB(t)
	newProjects(t, "acme")
	for _, title := range []string{"alpha", "beta", "legacy", "digits"} {
		if _, err := Add(title, "", 2, "acme", nil); err != nil {
			t.Fatalf("Add %s: %v", title, err)
		}
	}
	if _, err := db.Exec(uuidMigration); err != nil {
		t.Fatalf("uuidMigration: %v", err)
	}

	const (
		alpha  = "a3f90000-0000-4000-8000-000000000001"
		beta   = "a3f91111-0000-4000-8000-000000000002"
		legacy = "c0de0000-0000-4000-8000-000000000003"
		digits = "48210000-0000-4000-8000-000000000004"
	)
	pins := []struct {
		title, id string
		legacyID  int
	}{
		{"alpha", alpha, 1},
		{"beta", beta, 2},
		{"legacy", legacy, 47},
		{"digits", digits, 3},
	}
	for _, p := range pins {
		if _, err := db.Exec(`UPDATE tickets SET id = ?, legacy_id = ? WHERE title = ?`, p.id, p.legacyID, p.title); err != nil {
			t.Fatalf("pin %s: %v", p.title, err)
		}
	}
	// "4821" is now both alpha's legacy number and a prefix of digits' UUID.
	if _, err := db.Exec(`UPDATE tickets SET legacy_id = 4821 WHERE id = ?`, alpha); err != nil {
		t.Fatalf("pin legacy 4821: %v", err)
	}

	tests := []struct {
		ref     string
		want    string
		errHas  []string
		wantErr bool
	}{
		{ref: "a3f90", want: alpha},
		{ref: "ac-a3f91", want: beta},
		{ref: "A3F91", want: beta},
		{ref: "#a3f90", want: alpha},
		{ref: alpha, want: alpha},
		{ref: "ac-" + alpha, want: alpha},
		{ref: "c0de", want: legacy},
		{ref: "47", want: legacy},
		{ref: "ac-47", want: legacy},
		{ref: "a3f9", wantErr: true, errHas: []string{alpha, beta, "alpha", "beta"}},
		{ref: "4821", wantErr: true, errHas: []string{alpha, digits}},
		{ref: "ze-a3f90", wantErr: true, errHas: []string{"acme"}},
		{ref: "ffff", wantErr: true, errHas: []string{"no such ticket"}},
		{ref: "abc", wantErr: true, errHas: []string{"invalid id"}},
		{ref: "zzzz", wantErr: true, errHas: []string{"invalid id"}},
		{ref: "-a3f90", wantErr: true, errHas: []string{"invalid id"}},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			got, err := resolveUUIDRef(tt.ref)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveUUIDRef(%q) = %q, want error", tt.ref, got)
				}
				for _, s := range tt.errHas {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("error %q does not contain %q", err, s)
					}
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("resolveUUIDRef(%q) = (%q, %v), want %q", tt.ref, got, err, tt.want)
			}
		})
	}
}
