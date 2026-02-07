package runner

import (
	"time"
)

func DoWithRetry(retries int, backoff time.Duration, fn func() error) error {
	if retries < 0 { retries = 0 }
	var err error
	for attempt := 0; attempt <= retries; attempt++ {
		err = fn()
		if err == nil { return nil }
		if attempt == retries { break }
		sleep := backoff * time.Duration(1<<attempt)
		time.Sleep(sleep)
	}
	return err
}
