package main

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// scripted is a fake side that answers from a queue and records timeouts.
type scripted struct {
	replies  []reply
	timeouts []int
}

func (s *scripted) ask(timeout int) (reply, error) {
	s.timeouts = append(s.timeouts, timeout)
	r := s.replies[0]
	if len(s.replies) > 1 {
		s.replies = s.replies[1:]
	}
	return r, nil
}

func dataReply(t *testing.T, n int, status int) reply {
	return rep([]byte(`{"data":`+strconv.Itoa(n)+`}`), jsonType, status, nil)
}

func runCompare(t *testing.T, left, right *scripted, bracket bool) httpResult {
	t.Helper()
	c := testComparator(t)
	result, err := compareHTTP(left.ask, right.ask, func(a, b reply) ([]diff, error) {
		return c.responseDiffs(a, b, "/synthetic", nil, nil, nil)
	}, bracket)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestOneSidedTransportRetriesOnlyFailedSide(t *testing.T) {
	c := testComparator(t)
	for side := 0; side < 2; side++ {
		sides := [2]*scripted{{replies: []reply{dataReply(t, 2, 200)}}, {replies: []reply{dataReply(t, 2, 200)}}}
		sides[side] = &scripted{replies: []reply{transportFailure, transportFailure, dataReply(t, 2, 200)}}
		result := runCompare(t, sides[0], sides[1], false)
		assertDiffs(t, diffsOf(t, c, result.left, result.right, "/synthetic", nil), nil)
		want := [2]int{}
		want[side] = 2
		if result.retries != want {
			t.Fatalf("side %d retries %v", side, result.retries)
		}
		if !reflect.DeepEqual(sides[side].timeouts, []int{20, 40, 80}) || len(sides[1-side].timeouts) != 1 {
			t.Fatalf("side %d timeouts %v / %v", side, sides[side].timeouts, sides[1-side].timeouts)
		}
	}
}

func TestExhaustedTransportRetriesAreADifference(t *testing.T) {
	c := testComparator(t)
	for side := 0; side < 2; side++ {
		sides := [2]*scripted{{replies: []reply{jrep(t, "{}")}}, {replies: []reply{jrep(t, "{}")}}}
		sides[side] = &scripted{replies: []reply{transportFailure}}
		result := runCompare(t, sides[0], sides[1], true)
		assertDiffs(t, diffsOf(t, c, result.left, result.right, "/synthetic", nil), []diff{{"status", "transport-one-sided"}})
		if !reflect.DeepEqual(sides[side].timeouts, []int{20, 40, 80, 160}) || len(sides[1-side].timeouts) != 1 || result.rescued != 0 {
			t.Fatalf("side %d: %v %v %d", side, sides[side].timeouts, sides[1-side].timeouts, result.rescued)
		}
	}
}

func TestHTTPStatusDifferenceNeverRetries(t *testing.T) {
	c := testComparator(t)
	for _, bracket := range []bool{false, true} {
		for side := 0; side < 2; side++ {
			sides := [2]*scripted{{replies: []reply{jrep(t, "{}")}}, {replies: []reply{jrep(t, "{}")}}}
			sides[side] = &scripted{replies: []reply{rep([]byte("{}"), jsonType, 500, nil)}}
			result := runCompare(t, sides[0], sides[1], bracket)
			assertDiffs(t, diffsOf(t, c, result.left, result.right, "/synthetic", nil), []diff{{"status", "value"}})
			if len(sides[0].timeouts) != 1 || len(sides[1].timeouts) != 1 || result.retries != [2]int{} {
				t.Fatalf("bracket=%v side=%d", bracket, side)
			}
		}
	}
}

func TestBothSidedTransportDoesNotRetry(t *testing.T) {
	c := testComparator(t)
	for _, bracket := range []bool{false, true} {
		left, right := &scripted{replies: []reply{transportFailure}}, &scripted{replies: []reply{transportFailure}}
		result := runCompare(t, left, right, bracket)
		assertDiffs(t, diffsOf(t, c, result.left, result.right, "/synthetic", nil), []diff{{"status", "transport-both"}})
		if len(left.timeouts) != 1 || len(right.timeouts) != 1 {
			t.Fatal("retried")
		}
	}
}

func TestHTTPErrorAfterTransportRecoveryIsNotRetried(t *testing.T) {
	c := testComparator(t)
	left := &scripted{replies: []reply{transportFailure, rep([]byte("{}"), jsonType, 500, nil)}}
	right := &scripted{replies: []reply{jrep(t, "{}")}}
	result := runCompare(t, left, right, true)
	assertDiffs(t, diffsOf(t, c, result.left, result.right, "/synthetic", nil), []diff{{"status", "value"}})
	if len(left.timeouts) != 2 || len(right.timeouts) != 1 || result.retries != [2]int{1, 0} {
		t.Fatal(left.timeouts, right.timeouts, result.retries)
	}
}

func TestBracketStopsOnANewHTTPStatusDifference(t *testing.T) {
	c := testComparator(t)
	left := &scripted{replies: []reply{dataReply(t, 1, 200), dataReply(t, 2, 500)}}
	right := &scripted{replies: []reply{dataReply(t, 2, 200)}}
	result := runCompare(t, left, right, true)
	assertDiffs(t, diffsOf(t, c, result.left, result.right, "/synthetic", nil), []diff{{"status", "value"}})
	if len(left.timeouts) != 2 || len(right.timeouts) != 1 || result.rescued != 0 || result.retries != [2]int{} {
		t.Fatal(left.timeouts, right.timeouts)
	}
}

func TestBracketingAcceptsOnlyAnAdjacentReferenceState(t *testing.T) {
	compare := func(left, right []int) (int, int, int) {
		l, r := &scripted{}, &scripted{}
		for _, n := range left {
			l.replies = append(l.replies, dataReply(t, n, 200))
		}
		for _, n := range right {
			r.replies = append(r.replies, dataReply(t, n, 200))
		}
		result := runCompare(t, l, r, true)
		value := func(rp reply) int {
			v, _ := loadJSON(rp.Body)
			n, _ := getOr(v, "data", nil).(int64)
			return int(n)
		}
		return value(result.left), value(result.right), result.rescued
	}
	// The window moved between the first reference reply and Go: the next reference reply agrees.
	if a, b, n := compare([]int{1, 2}, []int{2}); a != 2 || b != 2 || n != 1 {
		t.Fatal(a, b, n)
	}
	// Go answered before the window moved, the reference after: the next Go reply agrees.
	if a, b, n := compare([]int{1, 2}, []int{0, 2}); a != 2 || b != 2 || n != 1 {
		t.Fatal(a, b, n)
	}
	// A Go reply that never equals any reference state is reported.
	if a, b, n := compare([]int{1, 1, 1, 1}, []int{9, 9, 9, 9}); a != 1 || b != 9 || n != 0 {
		t.Fatal(a, b, n)
	}
}

func TestBracketingAlternatesAfterReferenceTransportRecovery(t *testing.T) {
	c := testComparator(t)
	var calls []string
	queue := func(label string, replies ...reply) asker {
		return func(timeout int) (reply, error) {
			calls = append(calls, label+":"+strconv.Itoa(timeout))
			r := replies[0]
			replies = replies[1:]
			return r, nil
		}
	}
	result, err := compareHTTP(queue("base", transportFailure, dataReply(t, 2, 200), dataReply(t, 3, 200)),
		queue("go", dataReply(t, 1, 200), dataReply(t, 3, 200)),
		func(a, b reply) ([]diff, error) { return c.responseDiffs(a, b, "/synthetic", nil, nil, nil) }, true)
	if err != nil {
		t.Fatal(err)
	}
	assertDiffs(t, diffsOf(t, c, result.left, result.right, "/synthetic", nil), nil)
	if !reflect.DeepEqual(calls, []string{"base:20", "go:20", "base:40", "go:20", "base:20"}) || result.retries != [2]int{1, 0} {
		t.Fatal(calls, result.retries)
	}
}

func TestBracketTransportRetryBudgetIsPerCase(t *testing.T) {
	c := testComparator(t)
	left := &scripted{replies: []reply{transportFailure, dataReply(t, 1, 200), transportFailure, transportFailure, transportFailure}}
	right := &scripted{replies: []reply{dataReply(t, 2, 200), dataReply(t, 2, 200)}}
	result := runCompare(t, left, right, true)
	assertDiffs(t, diffsOf(t, c, result.left, result.right, "/synthetic", nil), []diff{{"status", "transport-one-sided"}})
	if result.retries != [2]int{3, 0} || !reflect.DeepEqual(left.timeouts, []int{20, 40, 20, 80, 160}) {
		t.Fatal(result.retries, left.timeouts)
	}
}

func TestNonBracketHTTPBodyDifferencesNeverRetry(t *testing.T) {
	c := testComparator(t)
	left, right := &scripted{replies: []reply{dataReply(t, 1, 200)}}, &scripted{replies: []reply{dataReply(t, 2, 200)}}
	result := runCompare(t, left, right, false)
	assertDiffs(t, diffsOf(t, c, result.left, result.right, "/synthetic", nil), []diff{{"body.data", "value"}})
	if len(left.timeouts) != 1 || len(right.timeouts) != 1 {
		t.Fatal("retried")
	}
}

func TestContentSyncTimeoutIsHarnessErrorWithoutClassification(t *testing.T) {
	failed := rep([]byte(`{"error":"proddiff content sync did not settle"}`), jsonType, 503, map[string]string{contentSyncHeader: "content-sync-timeout"})
	for side := 0; side < 2; side++ {
		sides := [2]*scripted{{replies: []reply{jrep(t, `{"status":"changed"}`)}}, {replies: []reply{jrep(t, `{"status":"changed"}`)}}}
		sides[side] = &scripted{replies: []reply{failed}}
		called := false
		_, err := compareHTTP(sides[0].ask, sides[1].ask, func(a, b reply) ([]diff, error) { called = true; return nil, nil }, true)
		if !errors.Is(err, errObserverTimeout) || called {
			t.Fatalf("side %d: %v called=%v", side, err, called)
		}
	}
}

func TestSendDistinguishesTransportFailuresFromHTTPErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/error":
			w.Header().Set("Content-Type", jsonType)
			w.Header().Add("X-Multi", "a")
			w.Header().Add("X-Multi", "b")
			w.WriteHeader(500)
			w.Write([]byte("{}"))
		case "/redirect":
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		case "/slow":
			time.Sleep(1500 * time.Millisecond)
		case "/echo":
			w.Header().Set("Content-Type", jsonType)
			w.Write([]byte(`{"host":"` + r.Host + `","auth":"` + r.Header.Get("Authorization") + `","encoding":"` + r.Header.Get("Accept-Encoding") + `"}`))
		}
	}))
	defer server.Close()
	port, _ := strconv.Atoi(server.URL[strings.LastIndex(server.URL, ":")+1:])
	answer, failure, err := send(port, "GET", "/error", "", nil, 20*time.Second)
	if err != nil || failure != nil || answer.Status != 500 || answer.Headers["x-multi"] != "a, b" || answer.ContentType != jsonType {
		t.Fatalf("error reply %+v %v %v", answer, failure, err)
	}
	answer, _, _ = send(port, "GET", "/redirect", "", nil, 20*time.Second)
	if answer.Status != http.StatusFound {
		t.Fatalf("redirect followed: %d", answer.Status)
	}
	answer, _, _ = send(port, "GET", "/echo", "secret", nil, 20*time.Second)
	if !strings.Contains(string(answer.Body), `"host":"127.0.0.1:5001"`) || !strings.Contains(string(answer.Body), `"auth":"Bearer secret"`) ||
		!strings.Contains(string(answer.Body), `"encoding":"identity"`) {
		t.Fatalf("request headers %s", answer.Body)
	}
	answer, failure, err = send(port, "GET", "/slow", "", nil, 200*time.Millisecond)
	if err != nil || answer.Status != 0 || failure == nil || failure.Type != "TimeoutError" {
		t.Fatalf("timeout %+v %+v %v", answer, failure, err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	answer, failure, err = send(closed, "GET", "/", "", nil, 2*time.Second)
	if err != nil || answer.Status != 0 || failure == nil || failure.Type != "ConnectionRefusedError" || failure.Errno == nil {
		t.Fatalf("refused %+v %+v %v", answer, failure, err)
	}
}

func TestScrubReplacesOnlyTheOwnDataCopy(t *testing.T) {
	own := "/runs/run-a/copies-b/go"
	scrubbed := scrub(rep([]byte(`{"path":"/runs/run-a/copies-b/go/app.log","other":"/runs/run-a/copies-b/base"}`), jsonType, 200,
		map[string]string{"x-file": "/runs/run-a/copies-b/go/x"}), own)
	if string(scrubbed.Body) != `{"path":"<DATA_DIR>/app.log","other":"/runs/run-a/copies-b/base"}` || scrubbed.Headers["x-file"] != "<DATA_DIR>/x" {
		t.Fatalf("%s %v", scrubbed.Body, scrubbed.Headers)
	}
}

func TestPhasesPutLogReadersLast(t *testing.T) {
	tests := []struct {
		c    testCase
		want int
	}{
		{testCase{Method: "GET", Route: "/api/agents"}, 0},
		{testCase{Method: "POST", Route: "/api/generate/mihomo"}, 1},
		{testCase{Method: "GET", Route: "/api/logs/tail"}, 2},
		{testCase{Method: "GET", Route: "/api/client-devices/discovery"}, -1},
	}
	for _, tt := range tests {
		if got := phase(tt.c); got != tt.want {
			t.Fatalf("%v: %d", tt.c, got)
		}
	}
}
