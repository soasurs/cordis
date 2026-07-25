package server

type relationshipCursorPayload struct {
	UserID int64 `json:"uid"`
	Type   int32 `json:"typ"`
	Time   int64 `json:"t"`
	ID     int64 `json:"i"`
}

func readCursor(has bool, value string) (string, error) {
	if !has {
		return "", nil
	}
	if value == "" {
		return "", errInvalidCursor
	}
	return value, nil
}
