package sevenzip

import (
	"runtime"
	"slices"

	"github.com/bodgit/sevenzip/internal/util"
	lru "github.com/hashicorp/golang-lru/v2"
)

type cacher struct {
	c    *lru.Cache[int64, util.SizeReadSeekCloser]
	noop bool
}

func newCachePool(noop bool) (*cacher, error) {
	if noop {
		return &cacher{nil, true}, nil
	}

	cache, err := lru.NewWithEvict(runtime.NumCPU(), func(key int64, value util.SizeReadSeekCloser) {
		if value != nil {
			value.Close()
		}
	})

	return &cacher{cache, false}, err
}

func (c *cacher) Get(offset int64) (util.SizeReadSeekCloser, bool) {
	if c.noop {
		return nil, false
	}

	if v, ok := c.c.Peek(offset); ok {
		c.c.Add(offset, nil)
		return v, c.c.Remove(offset)
	}

	keys := c.c.Keys()
	slices.SortFunc(keys, func(a, b int64) int {
		return int(a - b)
	})

	for _, k := range keys {
		// First key less than offset is the closest
		if k < offset {
			v, _ := c.c.Peek(k)
			c.c.Add(k, nil)
			return v, c.c.Remove(k)
		}
	}

	return nil, false
}

func (c *cacher) Add(offset int64, rc util.SizeReadSeekCloser) bool {
	if c.noop {
		rc.Close()
		return false
	}
	return c.c.Add(offset, rc)
}
