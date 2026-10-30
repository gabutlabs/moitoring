package models

import "errors"

var ErrUserNotFound = errors.New("user not found")

type User struct {
	ID           int64
	PasswordHash string
}
