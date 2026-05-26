package activities

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursor_RoundTrip(t *testing.T) {
	c := Cursor{
		OccurredAt: time.Now().UTC().Truncate(time.Microsecond),
		ActivityID: uuid.New(),
	}
	encoded := c.Encode()
	got, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor: %v", err)
	}
	if !got.OccurredAt.Equal(c.OccurredAt) {
		t.Fatalf("time round-trip mismatch: want %s, got %s", c.OccurredAt, got.OccurredAt)
	}
	if got.ActivityID != c.ActivityID {
		t.Fatalf("id round-trip mismatch: want %s, got %s", c.ActivityID, got.ActivityID)
	}
}

func TestDecodeCursor_Errors(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"not base64", "!!!!"},
		{"missing separator", "aGVsbG8"},
		{"bad time", "WW9sb3wxMTExMTExMS0xMTExLTExMTEtMTExMS0xMTExMTExMTExMTE"}, // "Yolo|<uuid>"
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeCursor(tc.raw); err == nil {
				t.Fatalf("expected error for %q", tc.raw)
			}
		})
	}
}

// paginate over-fetches by 1; if we get exactly limit+1 rows it must
// trim the last row AND emit a next_cursor pointing at the LAST row
// of the trimmed page (so the next query strict-less-thans past it).
func TestPaginate_NextCursorPointsToLastTrimmedRow(t *testing.T) {
	now := time.Now().UTC()
	mk := func(i int) Activity {
		return Activity{
			ID:         uuid.New(),
			OccurredAt: now.Add(-time.Duration(i) * time.Second),
		}
	}
	items := []Activity{mk(0), mk(1), mk(2), mk(3)} // 4 rows
	resp := paginate(items, 3)

	if len(resp.Items) != 3 {
		t.Fatalf("want 3 items, got %d", len(resp.Items))
	}
	if resp.NextCursor == "" {
		t.Fatal("want non-empty next_cursor")
	}
	cur, err := DecodeCursor(resp.NextCursor)
	if err != nil {
		t.Fatalf("DecodeCursor: %v", err)
	}
	want := items[2] // last item of the trimmed page
	if cur.ActivityID != want.ID {
		t.Fatalf("next_cursor should point at items[2].ID; got %s, want %s", cur.ActivityID, want.ID)
	}
}

// When the result set is short of the limit there's nothing else to
// fetch — next_cursor MUST be empty so the client stops paginating.
func TestPaginate_NoNextCursorOnPartialPage(t *testing.T) {
	now := time.Now().UTC()
	items := []Activity{
		{ID: uuid.New(), OccurredAt: now},
		{ID: uuid.New(), OccurredAt: now.Add(-time.Second)},
	}
	resp := paginate(items, 10)
	if len(resp.Items) != 2 {
		t.Fatalf("want 2 items, got %d", len(resp.Items))
	}
	if resp.NextCursor != "" {
		t.Fatal("partial page should have empty next_cursor")
	}
}

func TestBuildSummary(t *testing.T) {
	uid := uuid.New()
	cases := []struct {
		name string
		in   Activity
		want string
	}{
		{
			name: "booking created trainer scope",
			in: Activity{
				Type:  BookingCreated,
				Actor: &Actor{Name: "Jane", UserID: &uid},
			},
			want: "Jane booked a session with you",
		},
		{
			name: "booking created admin scope",
			in: Activity{
				Type:    BookingCreated,
				Actor:   &Actor{Name: "Jane"},
				Trainer: &TrainerRef{Name: "Mike"},
			},
			want: "Jane booked a session with Mike",
		},
		{
			name: "cancellation with reason",
			in: Activity{
				Type:  BookingCancelled,
				Actor: &Actor{Name: "Jane"},
				Extra: "schedule conflict",
			},
			want: "Jane cancelled their session with you — schedule conflict",
		},
		{
			name: "review",
			in: Activity{
				Type:  ReviewReceived,
				Actor: &Actor{Name: "Jane"},
				Extra: "5",
			},
			want: "Jane left a 5-star review for you",
		},
		{
			name: "no actor falls back to Someone",
			in: Activity{
				Type: BookingCreated,
			},
			want: "Someone booked a session with you",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildSummary(tc.in)
			if got != tc.want {
				t.Fatalf("summary mismatch:\nwant %q\ngot  %q", tc.want, got)
			}
		})
	}
}
