package aggregations_test

import (
	"configflow/internal/auth"
	"configflow/internal/repo"
	"configflow/internal/server"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFullApplicationRegistersAggregationRoutes(t *testing.T) {
	previous := auth.Configure(auth.Config{AdminUsername: "synthetic", AdminPassword: "synthetic", SecretKey: "synthetic-secret"})
	t.Cleanup(func() { auth.Configure(previous) })
	repository, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	app := server.NewApp() // Registers every module, including potentially overlapping wildcards.
	count := 0
	for _, route := range app.Router.Routes() {
		if strings.HasPrefix(route.Endpoint, "subscription_aggregations.") || route.Endpoint == "profiles.profile_aggregation_provider" {
			count += len(route.Methods)
		}
	}
	if count != 11 {
		t.Fatalf("registered %d methods, want 11", count)
	}
	for _, path := range []string{"/api/aggregations", "/api/aggregations/reorder", "/api/aggregations/a/health-impact", "/api/profiles/default/aggregations/a/provider"} {
		method := "GET"
		if strings.HasSuffix(path, "reorder") {
			method = "POST"
		}
		result := httptest.NewRecorder()
		request := httptest.NewRequest(method, path, nil)
		request = request.WithContext(repo.WithRepository(request.Context(), repository))
		app.ServeHTTP(result, request)
		if result.Code != 401 {
			t.Fatalf("%s %s status %d", method, path, result.Code)
		}
	}
}
