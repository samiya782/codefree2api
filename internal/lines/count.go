package lines

import "strconv"

// maxSafeInteger is the largest integer JavaScript keeps exact.
// codefree-o drops a userId that does not fit in it.
const maxSafeInteger = 9007199254740991

func parseCount(raw string) (int64, error) {
	if raw == "" || !digitsOnly(raw) {
		return 0, errNotInteger
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n > maxSafeInteger {
		return 0, errNotInteger
	}
	return n, nil
}

func digitsOnly(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
