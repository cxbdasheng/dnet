package helper

import "strings"

// SplitLines splits a string into lines by '\r\n' or '\n'.
func SplitLines(s string) []string {
	if strings.Contains(s, "\r\n") {
		return strings.Split(s, "\r\n")
	}

	return strings.Split(s, "\n")
}
