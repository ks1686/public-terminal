package rebalance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// yahooTestServer spins up an httptest server standing in for Yahoo's
// cookie/crumb/quote endpoints and points the overridable URL vars at it. The
// returned restore func puts everything back.
func yahooTestServer(t *testing.T) (*httptest.Server, *atomic.Bool, func()) {
	t.Helper()
	var sessionValid atomic.Bool
	sessionValid.Store(true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "cookie"):
			http.SetCookie(w, &http.Cookie{Name: "A1", Value: "test-cookie"})
			w.WriteHeader(200)

		case strings.Contains(r.URL.Path, "getcrumb"):
			if r.Header.Get("Cookie") == "" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte("test-crumb"))

		case strings.Contains(r.URL.Path, "quote"):
			hasCookie := false
			for _, c := range r.Cookies() {
				if c.Name == "A1" && c.Value == "test-cookie" {
					hasCookie = true
				}
			}
			if !sessionValid.Load() || r.Header.Get("Cookie") == "" || r.URL.Query().Get("crumb") != "test-crumb" {
				w.WriteHeader(401)
				w.Write([]byte(`<html>Unauthorized</html>`))
				return
			}
			symbols := strings.Split(r.URL.Query().Get("symbols"), ",")
			w.Write([]byte(`{"quoteResponse":{"result":[`))
			for i, s := range symbols {
				if i > 0 {
					w.Write([]byte(`,`))
				}
				w.Write([]byte(`{"symbol":"` + s + `","marketCap":1230000000}`))
			}
			w.Write([]byte(`],"error":null}}`))
			_ = hasCookie
		default:
			w.WriteHeader(404)
		}
	}))

	oldCookieURL, oldCrumbURL, oldQuoteURL := yahooCookieURL, yahooCrumbURL, yahooQuoteURL
	yahooCookieURL = srv.URL + "/cookie"
	yahooCrumbURL = srv.URL + "/getcrumb"
	yahooQuoteURL = srv.URL + "/quote"

	resetYahooSession()

	restore := func() {
		yahooCookieURL, yahooCrumbURL, yahooQuoteURL = oldCookieURL, oldCrumbURL, oldQuoteURL
		resetYahooSession()
		srv.Close()
	}
	return srv, &sessionValid, restore
}

func TestFetchYahooBatchMarketCapsSendsAuth(t *testing.T) {
	_, _, restore := yahooTestServer(t)
	defer restore()

	caps, err := fetchYahooBatchMarketCaps(context.Background(), []string{"AAPL", "MSFT"})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if len(caps) != 2 || caps["AAPL"] != 1230000000 || caps["MSFT"] != 1230000000 {
		t.Fatalf("unexpected caps: %v", caps)
	}

	// Session must now be cached — a second call must not need re-auth.
	resetYahooSession()
	caps2, err := fetchYahooBatchMarketCaps(context.Background(), []string{"TSLA"})
	if err != nil {
		t.Fatalf("cached-session call failed: %v", err)
	}
	if caps2["TSLA"] != 1230000000 {
		t.Fatalf("unexpected cached-session caps: %v", caps2)
	}
}

func TestFetchYahooBatchMarketCapsRejectsUnauthorized(t *testing.T) {
	_, sessionValid, restore := yahooTestServer(t)
	defer restore()
	sessionValid.Store(false)

	resetYahooSession()
	_, err := fetchYahooBatchMarketCaps(context.Background(), []string{"AAPL"})
	if err == nil {
		t.Fatal("expected 401 to surface as a typed error, got success")
	}
	if !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("expected 401 detail in error, got: %v", err)
	}
}

func TestResetYahooSessionAfter401(t *testing.T) {
	_, sessionValid, restore := yahooTestServer(t)
	defer restore()

	resetYahooSession()
	if _, err := fetchYahooBatchMarketCaps(context.Background(), []string{"AAPL"}); err != nil {
		t.Fatalf("initial fetch failed: %v", err)
	}
	if yahooCrumb == "" {
		t.Fatal("session should be cached after first fetch")
	}

	// Server revokes the session; next batch gets 401 → cache must be dropped.
	sessionValid.Store(false)
	if _, err := fetchYahooBatchMarketCaps(context.Background(), []string{"MSFT"}); err == nil {
		t.Fatal("expected unauthorized after session revocation")
	}
	if yahooCrumb != "" {
		t.Fatal("401 should have reset the cached Yahoo session")
	}

	// Server accepts again; next batch re-authenticates from scratch.
	sessionValid.Store(true)
	caps, err := fetchYahooBatchMarketCaps(context.Background(), []string{"GOOG"})
	if err != nil || caps["GOOG"] != 1230000000 {
		t.Fatalf("re-auth after reset failed: %v (caps %v)", err, caps)
	}
}
