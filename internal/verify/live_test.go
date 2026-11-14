package verify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLiveEpochReadsPublishedDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/deb/dists/stable/Release" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("Origin: moraxyc\nDate: Sat, 14 Nov 2026 16:56:07 +0000\nValid-Until: Sat, 21 Nov 2026 16:56:07 +0000\n"))
	}))
	defer server.Close()
	epoch, err := LiveEpoch(context.Background(), server.URL+"/deb")
	if err != nil {
		t.Fatal(err)
	}
	if epoch != 1794675367 {
		t.Fatalf("epoch = %d", epoch)
	}
}

func TestLiveEpochRejectsUnknownTrees(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing/dists/stable/Release":
			http.NotFound(w, r)
		default:
			_, _ = w.Write([]byte("Origin: moraxyc\n"))
		}
	}))
	defer server.Close()
	for _, base := range []string{server.URL + "/missing", server.URL + "/undated"} {
		if _, err := LiveEpoch(context.Background(), base); err == nil {
			t.Fatalf("%s: expected an error", base)
		}
	}
}
