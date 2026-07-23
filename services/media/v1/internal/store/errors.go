package store

import "errors"

var (
	ErrNotFound          = errors.New("asset not found")
	ErrActiveUploadLimit = errors.New("active upload limit reached")
)
