package events

import (
	"strings"

	"github.com/roadrunner-server/errors"
)

type wildcard struct {
	prefix string
	suffix string
}

func newWildcard(pattern string) (*wildcard, error) {
	// Normalize
	origin := strings.ToLower(pattern)
	before, after, ok := strings.Cut(origin, "*")

	/*
		http.*
		*
		*.WorkerError
	*/
	if !ok {
		dotI := strings.IndexByte(pattern, '.')

		if dotI == -1 {
			// http.SuperEvent
			return nil, errors.Str("wrong wildcard, no * or . Usage: http.Event or *.Event or http.*")
		}

		return &wildcard{origin[0:dotI], origin[dotI+1:]}, nil
	}

	// pref: http.
	// suff: *
	return &wildcard{before, after}, nil
}

func (w wildcard) match(s string) bool {
	s = strings.ToLower(s)
	return len(s) >= len(w.prefix)+len(w.suffix) && strings.HasPrefix(s, w.prefix) && strings.HasSuffix(s, w.suffix)
}
