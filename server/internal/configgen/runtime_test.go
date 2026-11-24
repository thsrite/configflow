package configgen

import (
	"context"
	"os"
	"testing"

	"configflow/internal/convert/mihomoenv"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
)

func TestGenerationOwnerRuntime(t *testing.T) {
	mihomoenv.OverrideForTest(t, nil, nil, nil)
	for _, engine := range []Engine{Mihomo, Surge, Loon, MosDNS, OxiDNS} {
		for _, shutdown := range []bool{true, false} {
			t.Run(string(engine)+map[bool]string{true: "/shutdown", false: "/disconnect"}[shutdown], func(t *testing.T) {
				r, err := repo.Open(t.TempDir(), repo.Options{})
				if err != nil {
					t.Fatal(err)
				}
				base := repo.WithScope(t.Context())
				rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
				rt.Start(t.Context())
				t.Cleanup(func() { rt.Shutdown(t.Context()) })
				request, cancel := context.WithCancel(base)
				defer cancel()
				ctx := rt.BindRequest(request)
				if shutdown {
					rt.Shutdown(t.Context())
				} else {
					cancel()
				}
				result, err := Build(ctx, r, "default", engine, "")
				if shutdown {
					if err == nil {
						t.Fatal("shutdown generated download")
					}
					entries, _ := os.ReadDir(r.ProfilesDir() + "/default/generated")
					if len(entries) > 0 {
						t.Fatal("shutdown wrote generated files")
					}
				} else if err != nil || (result.Path == "" && len(result.Data) == 0) {
					t.Fatalf("disconnect stopped generation: %v", err)
				}
			})
		}
	}
}
