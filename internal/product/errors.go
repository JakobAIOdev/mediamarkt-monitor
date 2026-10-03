package product

import (
	"fmt"
	"time"
)

// HTTPError preserves the response status and requested retry delay.
type HTTPError struct {
	PID        string
	StatusCode int
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("product %s: HTTP %d from MediaMarkt", e.PID, e.StatusCode)
}
