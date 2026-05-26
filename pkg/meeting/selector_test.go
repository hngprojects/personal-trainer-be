package meeting

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// StaticSelector should always return its Provider, never nil.
func TestStaticSelector_AlwaysReturnsProvider(t *testing.T) {
	s := StaticSelector{Provider: NoOp{}}
	got := s.For(context.Background(), uuid.New())
	if got == nil {
		t.Fatal("expected non-nil provider")
	}
	if _, ok := got.(NoOp); !ok {
		t.Fatalf("expected NoOp, got %T", got)
	}
}

// When constructed with a nil Provider, the selector should still
// return a usable Provider (NoOp) rather than nil — call sites do
// not nil-check the result.
func TestStaticSelector_NilProviderReturnsNoOp(t *testing.T) {
	s := StaticSelector{}
	got := s.For(context.Background(), uuid.New())
	if got == nil {
		t.Fatal("expected NoOp fallback, got nil")
	}
	if _, ok := got.(NoOp); !ok {
		t.Fatalf("expected NoOp fallback, got %T", got)
	}
}
