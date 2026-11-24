package cmds

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchImageReturnsErrorForNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	contentType, body, err := fetchImage(server.URL)
	if err == nil {
		t.Fatal("expected an error for a non-200 response")
	}
	if !strings.Contains(err.Error(), "403 Forbidden") {
		t.Fatalf("error %q does not include the response status", err)
	}
	if contentType != nil {
		t.Fatalf("expected nil content type, got %q", *contentType)
	}
	if body != nil {
		t.Fatalf("expected nil body, got %d bytes", len(body))
	}
}
