package run

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
)

// Shard is the K-th (counting from 1) of N groups of mutants split by a hash of their identifiers.
// Splitting by identifier keeps the other mutants' assignments when mutants are added or removed, so each shard's cache stays effective.
type Shard struct {
	K, N uint64
}

// ParseShard reads a specification of the form k/n.
func ParseShard(s string) (*Shard, error) {
	ks, ns, ok := strings.Cut(s, "/")
	k, kerr := strconv.ParseUint(ks, 10, 64)
	n, nerr := strconv.ParseUint(ns, 10, 64)
	if !ok || kerr != nil || nerr != nil || k < 1 || k > n {
		return nil, fmt.Errorf("invalid -shard %q; use k/n with 1 <= k <= n", s)
	}
	return &Shard{K: k, N: n}, nil
}

// Contains reports whether the mutant with identifier id belongs to this shard. When s is nil, every mutant does.
func (s *Shard) Contains(id string) bool {
	if s == nil {
		return true
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return h.Sum64()%s.N == s.K-1
}
