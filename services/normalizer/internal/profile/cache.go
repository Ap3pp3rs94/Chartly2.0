package profile

import (

	"context"

	"os"

	"path/filepath"

	"strings"

	"sync"

	"time"
)

type Cache struct {

	loader *Loader

	ttl    time.Duration


	mu sync.Mutex


	profiles         []Profile

	loadedAt         time.Time

	lastScanMaxMTime time.Time
}

func NewCache(loader *Loader) *Cache {

	if loader == nil {


		loader = NewLoader("")

	}

	return &Cache{


		loader: loader,


		ttl:    30 * time.Second,

	}
}

func (c *Cache) WithTTL(d time.Duration) *Cache {

	if d > 0 {


		c.ttl = d

	}

	return c
}

func (c *Cache) Invalidate() {

	c.mu.Lock()
	defer c.mu.Unlock()

	c.loadedAt = time.Time{}
	c.lastScanMaxMTime = time.Time{}
	c.profiles = nil
}

func (c *Cache) Get(ctx context.Context) ([]Profile, error) {

	_ = ctx


	c.mu.Lock()
	defer c.mu.Unlock()


	// If cache is warm and TTL not expired, return.

	if !c.loadedAt.IsZero() && time.Since(c.loadedAt) < c.ttl {


		out := make([]Profile, len(c.profiles))


		copy(out, c.profiles)


		return out, nil

	}


	// TTL expired: scan directory max mtime to see if any changes.

	maxM, err := scanMaxMTime(c.loader.baseDir)

	if err == nil && !c.lastScanMaxMTime.IsZero() && maxM.Equal(c.lastScanMaxMTime) && c.profiles != nil {


		// unchanged; refresh loadedAt and return cached


		c.loadedAt = time.Now()


		out := make([]Profile, len(c.profiles))


		copy(out, c.profiles)


		return out, nil

	}


	// reload

	ps, err := c.loader.LoadAll()

	if err != nil {


		return nil, err

	}

	c.profiles = ps

	c.loadedAt = time.Now()

	c.lastScanMaxMTime = maxM


	out := make([]Profile, len(c.profiles))

	copy(out, c.profiles)

	return out, nil
}

func scanMaxMTime(dir string) (time.Time, error) {

	dir = strings.TrimSpace(dir)

	if dir == "" {


		dir = "./profiles"

	}


	ents, err := os.ReadDir(dir)

	if err != nil {


		return time.Time{}, err

	}


	var max time.Time

	for _, e := range ents {


		if e.IsDir() {



			continue


		}


		name := strings.ToLower(e.Name())


		if !strings.HasSuffix(name, ".json") {



			continue


		}


		info, err := e.Info()


		if err != nil {



			continue


		}


		mt := info.ModTime()


		if mt.After(max) {



			max = mt


		}

	}


	// Normalize to filesystem resolution by truncating to seconds.

	// This reduces false positives on some FS.

	max = max.Truncate(time.Second)

	return max, nil
}

// ensure filepath imported (used in other builds) - keep to avoid lint.
var _ = filepath.Separator
