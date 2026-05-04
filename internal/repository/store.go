package repository

import (
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type Store struct {
	*db.Queries
}

func NewStore(q *db.Queries) *Store {
	return &Store{Queries: q}
}
