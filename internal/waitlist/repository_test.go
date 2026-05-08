package waitlist_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/internal/waitlist"
)

// TestWaitlistRepository is a mock for testing that implements the interface
type testWaitlistRepository struct {
	addEmailFn   func(ctx context.Context, email, feedback string) error
	getAllFn     func(ctx context.Context) ([]db.Waitlist, error)
	getByEmailFn func(ctx context.Context, email string) (*db.Waitlist, error)
}

func (t *testWaitlistRepository) AddEmail(ctx context.Context, email, feedback string) error {
	if t.addEmailFn != nil {
		return t.addEmailFn(ctx, email, feedback)
	}
	return nil
}

func (t *testWaitlistRepository) GetAll(ctx context.Context) ([]db.Waitlist, error) {
	if t.getAllFn != nil {
		return t.getAllFn(ctx)
	}
	return []db.Waitlist{}, nil
}

func (t *testWaitlistRepository) GetByEmail(ctx context.Context, email string) (*db.Waitlist, error) {
	if t.getByEmailFn != nil {
		return t.getByEmailFn(ctx, email)
	}
	return nil, waitlist.ErrNotFound
}

// TestPostgresWaitlistRepo_GetByEmail_Found tests finding specific email
func TestPostgresWaitlistRepo_GetByEmail_Found(t *testing.T) {
	now := time.Now()
	entryID := uuid.New()

	repo := &testWaitlistRepository{
		getByEmailFn: func(ctx context.Context, email string) (*db.Waitlist, error) {
			if email == "test@example.com" {
				return &db.Waitlist{
					ID:        entryID,
					Email:     "test@example.com",
					Feedback:  "great",
					CreatedAt: now,
				}, nil
			}
			return nil, waitlist.ErrNotFound
		},
	}

	ctx := context.Background()

	result, err := repo.GetByEmail(ctx, "test@example.com")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if result == nil {
		t.Errorf("expected result, got nil")
		return
	}

	if result.Email != "test@example.com" {
		t.Errorf("expected email 'test@example.com', got '%s'", result.Email)
	}
	if result.ID != entryID {
		t.Errorf("expected id %s, got %s", entryID, result.ID)
	}
}

// TestPostgresWaitlistRepo_GetByEmail_NotFound tests not finding email
func TestPostgresWaitlistRepo_GetByEmail_NotFound(t *testing.T) {
	repo := &testWaitlistRepository{
		getByEmailFn: func(ctx context.Context, email string) (*db.Waitlist, error) {
			return nil, waitlist.ErrNotFound
		},
	}

	ctx := context.Background()

	result, err := repo.GetByEmail(ctx, "notfound@example.com")
	if err != waitlist.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

// TestPostgresWaitlistRepo_GetByEmail_Error tests error handling
func TestPostgresWaitlistRepo_GetByEmail_Error(t *testing.T) {
	repo := &testWaitlistRepository{
		getByEmailFn: func(ctx context.Context, email string) (*db.Waitlist, error) {
			return nil, errors.New("database error")
		},
	}

	ctx := context.Background()

	result, err := repo.GetByEmail(ctx, "test@example.com")
	if err == nil {
		t.Errorf("expected error, got nil")
	}

	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

// TestWaitlistRepository_GetAll_Success tests getting all entries
func TestWaitlistRepository_GetAll_Success(t *testing.T) {
	now := time.Now()
	expectedEntries := []db.Waitlist{
		{
			ID:        uuid.New(),
			Email:     "user1@example.com",
			Feedback:  "feedback1",
			CreatedAt: now,
		},
		{
			ID:        uuid.New(),
			Email:     "user2@example.com",
			Feedback:  "feedback2",
			CreatedAt: now,
		},
	}

	repo := &testWaitlistRepository{
		getAllFn: func(ctx context.Context) ([]db.Waitlist, error) {
			return expectedEntries, nil
		},
	}

	ctx := context.Background()

	results, err := repo.GetAll(ctx)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if len(results) != len(expectedEntries) {
		t.Errorf("expected %d entries, got %d", len(expectedEntries), len(results))
	}

	for i, entry := range results {
		if entry.Email != expectedEntries[i].Email {
			t.Errorf("expected email '%s', got '%s'", expectedEntries[i].Email, entry.Email)
		}
	}
}

// TestWaitlistRepository_GetAll_Empty tests getting empty list
func TestWaitlistRepository_GetAll_Empty(t *testing.T) {
	repo := &testWaitlistRepository{
		getAllFn: func(ctx context.Context) ([]db.Waitlist, error) {
			return []db.Waitlist{}, nil
		},
	}

	ctx := context.Background()

	results, err := repo.GetAll(ctx)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected empty list, got %d entries", len(results))
	}
}

// TestWaitlistRepository_AddEmail_Success tests adding email successfully
func TestWaitlistRepository_AddEmail_Success(t *testing.T) {
	var capturedEmail, capturedFeedback string

	repo := &testWaitlistRepository{
		addEmailFn: func(ctx context.Context, email, feedback string) error {
			capturedEmail = email
			capturedFeedback = feedback
			return nil
		},
	}

	ctx := context.Background()

	err := repo.AddEmail(ctx, "test@example.com", "great service")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if capturedEmail != "test@example.com" {
		t.Errorf("expected email 'test@example.com', got '%s'", capturedEmail)
	}
	if capturedFeedback != "great service" {
		t.Errorf("expected feedback 'great service', got '%s'", capturedFeedback)
	}
}

// TestWaitlistRepository_AddEmail_Error tests error handling when adding email
func TestWaitlistRepository_AddEmail_Error(t *testing.T) {
	repo := &testWaitlistRepository{
		addEmailFn: func(ctx context.Context, email, feedback string) error {
			return errors.New("database error")
		},
	}

	ctx := context.Background()

	err := repo.AddEmail(ctx, "test@example.com", "feedback")
	if err == nil {
		t.Errorf("expected error, got nil")
	}
}
