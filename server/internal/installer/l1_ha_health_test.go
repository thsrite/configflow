package installer

import (
	"testing"

	"configflow/internal/installer/installertest"
	"configflow/internal/jsonx"
)

func l1GuideBaseline(t *testing.T, result *jsonx.Object) *jsonx.Object {
	t.Helper()
	if result == nil {
		return nil
	}
	raw, err := jsonx.Response(result)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = installertest.GuideBaseline(raw)
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value.(*jsonx.Object)
}

func TestL1HAHealthSourceMatchesApprovedDelta(t *testing.T) {
	want, err := installertest.HAStartupModule()
	if err != nil || want != haStartupChecks {
		t.Fatalf("startup module drift: %v", err)
	}
}

func TestL1HARefusesBusyPortAndFatalCore(t *testing.T) {
	for _, problem := range []string{"port_busy", "fatal"} {
		t.Run(problem, func(t *testing.T) {
			tmp := t.TempDir()
			_, host, guide := newHost(t, tmp, "host", problem, true)
			r := runCommand(t, guide.GetString("install_command"), tmp, host.env...)
			if r.code == 0 {
				t.Fatal("unhealthy installation reported success")
			}
			notContains(t, r.stdout, "本次安装的容器已启动或恢复")
			if problem == "fatal" {
				contains(t, r.stderr, "OxiDNS 未正常运行")
			} else {
				contains(t, r.stderr, "53 端口不可用")
			}
		})
	}
}
