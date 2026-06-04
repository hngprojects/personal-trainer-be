package settings

import "testing"

// slugify covers the bulk of admin-side input that won't already be
// validated by the slugRegex (admins typing display names like
// "Weight loss" expect a sane default — this function decides what
// that default looks like). Worth pinning so the wire shape doesn't
// silently drift.
func TestSlugify(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"single word lowercases", "Strength", "strength"},
		{"spaces become single dash", "Weight loss", "weight-loss"},
		{"multiple spaces collapse", "Weight   loss", "weight-loss"},
		{"underscore becomes dash", "weight_loss", "weight-loss"},
		{"trailing punctuation stripped", "Yoga!", "yoga"},
		{"mixed punctuation stripped", "HI/IT", "hiit"},
		{"unicode stripped (ASCII-only by design)", "Café", "caf"},
		{"leading/trailing spaces trimmed", "  Mobility  ", "mobility"},
		{"hyphen preserved", "weight-loss", "weight-loss"},
		{"double hyphen collapses", "weight--loss", "weight-loss"},
		{"empty in, empty out", "", ""},
		{"only punctuation in, empty out", "!!!", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := slugify(tc.in)
			if got != tc.want {
				t.Fatalf("slugify(%q): want %q, got %q", tc.in, tc.want, got)
			}
		})
	}
}

// The slug we accept on input MUST be matchable by slugRegex.
// Without this guard, slugify could happily generate a value that the
// validate step then rejects.
func TestSlugifyOutputMatchesSlugRegex(t *testing.T) {
	inputs := []string{
		"Strength", "Yoga", "HIIT", "Pilates", "Endurance",
		"Weight loss", "Mobility",
		"Plank Hold", "Marathon Training",
		"core_strength",
	}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			got := slugify(in)
			if got == "" {
				t.Skipf("slugify produced empty string for %q (acceptable for malformed input)", in)
			}
			if !slugRegex.MatchString(got) {
				t.Fatalf("slugify(%q) = %q which does NOT match slugRegex — handler would 400 its own default", in, got)
			}
		})
	}
}

// nullInt32 / nullBool are the seam between the JSON-pointer DTO and
// sqlc's sql.NullX params. Pin the behaviour because a regression
// here would silently overwrite settings to zero/false instead of
// leaving them alone.
func TestNullInt32(t *testing.T) {
	if got := nullInt32(nil); got.Valid {
		t.Fatalf("nil pointer must produce !Valid; got %+v", got)
	}
	v := int32(42)
	got := nullInt32(&v)
	if !got.Valid || got.Int32 != 42 {
		t.Fatalf("pointer to 42 must produce {Valid:true, Int32:42}; got %+v", got)
	}
	// Zero value is a real value, NOT "no change".
	zero := int32(0)
	got = nullInt32(&zero)
	if !got.Valid || got.Int32 != 0 {
		t.Fatalf("pointer to 0 must produce {Valid:true, Int32:0} — never confuse with nil; got %+v", got)
	}
}

func TestNullBool(t *testing.T) {
	if got := nullBool(nil); got.Valid {
		t.Fatalf("nil pointer must produce !Valid; got %+v", got)
	}
	tr, fl := true, false
	if got := nullBool(&tr); !got.Valid || !got.Bool {
		t.Fatalf("pointer to true must produce {Valid:true, Bool:true}; got %+v", got)
	}
	if got := nullBool(&fl); !got.Valid || got.Bool {
		t.Fatalf("pointer to false must produce {Valid:true, Bool:false} — never confuse with nil; got %+v", got)
	}
}
