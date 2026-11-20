package provider

import (
	"strings"
	"testing"
)

func TestConfigFindingsPreparationContextWithoutA11Positions(t *testing.T) {
	t.Cleanup(SetLocations(nil))
	_, err := ParseNodes("proxies: [bad]", "Synthetic", "")
	if err == nil {
		t.Fatal("invalid provider accepted")
	}
	for _, text := range []string{"订阅「Synthetic」准备失败：Provider 代理必须是含非空 name 的对象", "字段：proxies[0]", "内容来源：本次订阅返回的内容"} {
		if !strings.Contains(err.Error(), text) {
			t.Errorf("missing %q: %v", text, err)
		}
	}
}
