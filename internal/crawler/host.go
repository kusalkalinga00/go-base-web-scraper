package crawler

import "strings"

// sameHost reports whether two hostnames identify the same site, treating
// "www.example.com" and "example.com" as equal.
func sameHost(a, b string) bool {
	return strings.TrimPrefix(strings.ToLower(a), "www.") == strings.TrimPrefix(strings.ToLower(b), "www.")
}
