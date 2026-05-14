package routes

import db "github.com/hngprojects/personal-trainer-be/internal/repository/db"

type usersStore struct {
	q *db.Queries
}

func newUsersStore(q *db.Queries) *usersStore { return &usersStore{q: q} }
