package uuid

import "github.com/google/uuid"

type IDer struct{}

func (IDer) ID() string { return uuid.NewString() }
