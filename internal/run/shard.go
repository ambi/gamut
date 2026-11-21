package run

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
)

// Shard は、変異体を識別子のハッシュで N 個に分けたうちの K 番目（1 から数える）である。
// 識別子で分けるので、変異体が増えても減っても、ほかの変異体の割り当ては変わらず、シャードごとのキャッシュが効き続ける。
type Shard struct {
	K, N uint64
}

// ParseShard は、k/n の形の指定を読む。
func ParseShard(s string) (*Shard, error) {
	ks, ns, ok := strings.Cut(s, "/")
	k, kerr := strconv.ParseUint(ks, 10, 64)
	n, nerr := strconv.ParseUint(ns, 10, 64)
	if !ok || kerr != nil || nerr != nil || k < 1 || k > n {
		return nil, fmt.Errorf("invalid -shard %q; use k/n with 1 <= k <= n", s)
	}
	return &Shard{K: k, N: n}, nil
}

// Contains は、識別子 id の変異体がこのシャードに入るかを返す。s が nil なら、どの変異体も入る。
func (s *Shard) Contains(id string) bool {
	if s == nil {
		return true
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return h.Sum64()%s.N == s.K-1
}
