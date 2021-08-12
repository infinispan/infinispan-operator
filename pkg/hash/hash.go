package hash

import (
	"crypto/sha1"
	"encoding/hex"
	"sort"
)

func HashString(data string) string {
	hash := sha1.New()
	hash.Write([]byte(data))
	return hex.EncodeToString(hash.Sum(nil))
}

func HashByte(data []byte) string {
	hash := sha1.New()
	hash.Write(data)
	return hex.EncodeToString(hash.Sum(nil))
}

func HashMap(m map[string][]byte) string {
	hash := sha1.New()
	// Sort the map keys to ensure that the iteration order is the same on each call
	// Without this the computed sha will be different if the iteration order of the keys changes
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := m[k]
		hash.Write([]byte(k))
		hash.Write(v)
	}
	return hex.EncodeToString(hash.Sum(nil))
}
