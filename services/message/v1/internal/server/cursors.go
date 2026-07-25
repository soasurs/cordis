package server

type dmChannelsPayload struct {
	UserID int64 `json:"uid"`
	ID     int64 `json:"id"`
}

func readCursor(has bool, value string) (string, error) {
	if !has {
		return "", nil
	}
	if value == "" {
		return "", invalidRequest("cursor is invalid")
	}
	return value, nil
}
