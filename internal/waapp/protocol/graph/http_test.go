package graph

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuthenticatedGraphQLDoesNotFollowRedirects(t *testing.T) {
	var forwarded atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	_, err := Post(t.Context(), origin.Client(), origin.URL, Request{AccessToken: "synthetic-token", DocumentID: 1, Variables: struct{}{}})
	if err == nil || forwarded.Load() {
		t.Fatal("GraphQL credentials were forwarded to a redirect target")
	}
}

func TestGraphQLCancellationReleasesTheRequest(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err := Post(ctx, server.Client(), server.URL, Request{AccessToken: "synthetic-token", DocumentID: 1, Variables: struct{}{}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("GraphQL cancellation lost its deadline")
	}
}
