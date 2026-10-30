package graph

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

type UpstreamError struct{ Status int }

func (e UpstreamError) Error() string {
	if e.Status == 0 {
		return "GraphQL upstream is unreachable"
	}
	return fmt.Sprintf("GraphQL upstream HTTP %d", e.Status)
}

func Retryable(err error) bool {
	var rejection ResponseError
	if errors.As(err, &rejection) {
		return rejection.Extensions.Retryable
	}
	var upstream UpstreamError
	if errors.As(err, &upstream) {
		return upstream.Status == 0 || upstream.Status >= 500 || upstream.Status == http.StatusTooManyRequests
	}
	return errors.Is(err, context.DeadlineExceeded)
}
