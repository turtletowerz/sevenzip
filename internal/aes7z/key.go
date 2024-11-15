package aes7z

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

type cacheKey struct {
	password string
	cycles   int
	salt     string // []byte isn't comparable
}

const cacheSize = 10

//nolint:gochecknoglobals
var (
	once  sync.Once
	cache *lru.Cache[cacheKey, []byte]
)

func calculateKey(password string, cycles int, salt []byte) ([]byte, error) {
	once.Do(func() {
		// NOTE: We can ignore the error because cache size is guaranteed to not be 0
		cache, _ = lru.New[cacheKey, []byte](cacheSize)
	})

	ck := cacheKey{
		password: password,
		cycles:   cycles,
		salt:     hex.EncodeToString(salt),
	}

	if key, ok := cache.Get(ck); ok {
		return key, nil
	}

	b := bytes.NewBuffer(salt)

	// Convert password to UTF-16LE
	utf16le := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
	t := transform.NewWriter(b, utf16le.NewEncoder())
	_, _ = t.Write([]byte(password))

	key := make([]byte, sha256.Size)
	if cycles == 0x3f {
		copy(key, b.Bytes())
	} else {
		h := sha256.New()
		for i := uint64(0); i < 1<<cycles; i++ {
			// These will never error
			_, _ = h.Write(b.Bytes())
			_ = binary.Write(h, binary.LittleEndian, i)
		}

		copy(key, h.Sum(nil))
	}

	_ = cache.Add(ck, key)

	return key, nil
}
