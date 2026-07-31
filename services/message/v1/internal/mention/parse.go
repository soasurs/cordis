// Package mention parses user, role, and @everyone mentions from message
// content. The parser is intentionally conservative: anything that does not
// match exactly stays in the content as plain text.
package mention

import "strconv"

// Set is the parsed mention result for one message body. UserIDs and RoleIDs
// keep first-appearance order and contain no duplicates.
type Set struct {
	UserIDs  []int64
	RoleIDs  []int64
	Everyone bool
}

// Parse extracts user (<@ID>, deprecated <@!ID>), role (<@&ID>), and
// @everyone mentions. @everyone must be a complete lowercase word: the
// character before and after it may not be a letter, digit, or underscore.
// A mention preceded by an odd number of backslashes is escaped and stays
// as plain text.
func Parse(content string) Set {
	set := Set{}
	seenUsers := make(map[int64]struct{})
	seenRoles := make(map[int64]struct{})

	for i := 0; i < len(content); {
		if isEscaped(content, i) {
			i++
			continue
		}
		switch {
		case content[i] == '<' && i+1 < len(content) && content[i+1] == '@':
			id, role, end, ok := parseAngleMention(content, i)
			if !ok {
				i++
				continue
			}
			if role {
				if _, seen := seenRoles[id]; !seen {
					seenRoles[id] = struct{}{}
					set.RoleIDs = append(set.RoleIDs, id)
				}
			} else {
				if _, seen := seenUsers[id]; !seen {
					seenUsers[id] = struct{}{}
					set.UserIDs = append(set.UserIDs, id)
				}
			}
			i = end
		case content[i] == '@' && isEveryoneAt(content, i):
			set.Everyone = true
			i += len("@everyone")
		default:
			i++
		}
	}
	return set
}

// parseAngleMention parses <@ID>, <@!ID>, or <@&ID> starting at the '<'.
// It returns the target ID, whether it is a role mention, the index just
// past the closing '>', and whether the markup was well-formed.
func parseAngleMention(content string, i int) (id int64, role bool, end int, ok bool) {
	j := i + 2
	switch {
	case j < len(content) && content[j] == '!':
		j++
	case j < len(content) && content[j] == '&':
		role = true
		j++
	}
	digitsStart := j
	for j < len(content) && content[j] >= '0' && content[j] <= '9' {
		j++
	}
	if j == digitsStart || j >= len(content) || content[j] != '>' {
		return 0, false, 0, false
	}
	value, err := strconv.ParseInt(content[digitsStart:j], 10, 64)
	if err != nil || value <= 0 {
		return 0, false, 0, false
	}
	return value, role, j + 1, true
}

// isEveryoneAt reports whether content[i:] starts with the standalone word
// @everyone.
func isEveryoneAt(content string, i int) bool {
	const word = "@everyone"
	if i+len(word) > len(content) || content[i:i+len(word)] != word {
		return false
	}
	if i > 0 && isIdentRune(content[i-1]) {
		return false
	}
	end := i + len(word)
	return end == len(content) || !isIdentRune(content[end])
}

func isIdentRune(ch byte) bool {
	return ch >= 'a' && ch <= 'z' ||
		ch >= 'A' && ch <= 'Z' ||
		ch >= '0' && ch <= '9' ||
		ch == '_'
}

// isEscaped reports whether content[i] is preceded by an odd number of
// consecutive backslashes.
func isEscaped(content string, i int) bool {
	slashes := 0
	for j := i - 1; j >= 0 && content[j] == '\\'; j-- {
		slashes++
	}
	return slashes%2 == 1
}
