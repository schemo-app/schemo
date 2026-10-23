package platform

import "github.com/google/uuid"

type IDGen func() string

func NewIDGen() IDGen {
	return newID
}

func newID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}

	return id.String()
}
