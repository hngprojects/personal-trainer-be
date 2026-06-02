package meeting

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

type stubZoomSel struct{ p Provider }

func (s stubZoomSel) For(_ context.Context, _ uuid.UUID, _ string) Provider {
	if s.p == nil {
		return NoOp{}
	}
	return s.p
}

// MultiPlatformSelector must dispatch to the correct backend based on
// the platform string. The two platforms have different sub-selectors
// (Zoom has per-trainer; Meet is single org provider) — the
// MultiPlatform layer is what hides that asymmetry from callers.
func TestMultiPlatformSelector_Routes(t *testing.T) {
	zoomProvider := makeNoOpWithTag("zoom-provider")
	meetProvider := makeNoOpWithTag("meet-provider")

	mux := MultiPlatformSelector{
		Zoom: stubZoomSel{p: zoomProvider},
		Meet: meetProvider,
	}

	cases := []struct {
		name       string
		platform   string
		wantNoOp   bool
		wantTagged Provider
	}{
		{"zoom routes to Zoom selector", PlatformZoom, false, zoomProvider},
		{"google_meet routes to Meet provider", PlatformGoogleMeet, false, meetProvider},
		{"unknown platform returns NoOp", "messenger", true, nil},
		{"empty platform returns NoOp", "", true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mux.For(context.Background(), uuid.Nil, tc.platform)
			if tc.wantNoOp {
				if _, ok := got.(NoOp); !ok {
					t.Fatalf("want NoOp, got %T", got)
				}
				return
			}
			if got != tc.wantTagged {
				t.Fatalf("wrong provider: got %v, want %v", got, tc.wantTagged)
			}
		})
	}
}

// Nil sub-fields must NOT panic — config may load with one platform
// off and another on. NoOp is the safe default.
func TestMultiPlatformSelector_NilFieldsReturnNoOp(t *testing.T) {
	mux := MultiPlatformSelector{} // both nil

	for _, platform := range []string{PlatformZoom, PlatformGoogleMeet, "anything"} {
		t.Run(platform, func(t *testing.T) {
			got := mux.For(context.Background(), uuid.Nil, platform)
			if _, ok := got.(NoOp); !ok {
				t.Fatalf("want NoOp when sub-selector is nil, got %T", got)
			}
		})
	}
}

// StaticSelector now takes a platform argument too. It should still
// return its Provider regardless of platform (legacy behavior).
func TestStaticSelector_IgnoresPlatform(t *testing.T) {
	p := makeNoOpWithTag("static")
	s := StaticSelector{Provider: p}
	for _, platform := range []string{PlatformZoom, PlatformGoogleMeet, "anything"} {
		if got := s.For(context.Background(), uuid.New(), platform); got != p {
			t.Fatalf("platform %q: want %v, got %v", platform, p, got)
		}
	}
}

// makeNoOpWithTag returns a Provider we can identity-compare in tests.
// NoOp by value would compare equal across calls; using an anonymous
// pointer-backed type lets each call site stay distinct.
type taggedProv struct {
	tag string
	NoOp
}

func makeNoOpWithTag(tag string) Provider { return &taggedProv{tag: tag} }
