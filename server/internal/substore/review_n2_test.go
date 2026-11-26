package substore

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression (review N2): a subscription URL with Chinese path/query
// characters, an encoded "/" and a password net/url refuses in user
// information (requests accepted it) is requested with the same path and
// query bytes and the typed credentials.
func TestReviewN2RealisticSubscriptionURL(t *testing.T) {
	withEnviron(t)
	var uri, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uri, auth = r.RequestURI, r.Header.Get("Authorization")
		w.Write([]byte("proxies: []"))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	raw := "http://user:pa^ss|w@" + host + "/订阅/a%2Fb?name=机场 一&token=a|b&x=1%"
	resp, err := RequestsClient{}.Do(context.Background(), &Request{Method: "GET", URL: raw, Timeout: Seconds(5)})
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("request failed: %v", err)
	}
	if want := "/%E8%AE%A2%E9%98%85/a%2Fb?name=%E6%9C%BA%E5%9C%BA%20%E4%B8%80&token=a%7Cb&x=1%25"; uri != want {
		t.Errorf("server saw %q, want %q", uri, want)
	}
	if want := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pa^ss|w")); auth != want {
		t.Errorf("Authorization %q, want %q", auth, want)
	}
}

// Regression (review N2): a proxy password with characters net/url refuses
// in user information still reaches an explicitly configured proxy.
func TestReviewN2ProxyPasswordCharacters(t *testing.T) {
	direct := newFakeSubStore(t)
	proxy := newRecordingProxy(t)
	target := direct.srv.URL + "/api/utils/env"
	withEnviron(t)
	resp, err := RequestsClient{}.Do(context.Background(), &Request{Method: "GET", URL: target, Timeout: Seconds(5), Proxy: "http://user:p^w@" + strings.TrimPrefix(proxy.srv.URL, "http://")})
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("request through proxy: %v", err)
	}
	want := "GET " + target + " auth=Basic " + base64.StdEncoding.EncodeToString([]byte("user:p^w"))
	if seen := proxy.requests(); len(seen) != 1 || seen[0] != want {
		t.Fatalf("proxy saw %q, want %q", seen, want)
	}
}
