package agents

import (
	"encoding/json"
	"errors"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

// A05 review: set_agent_traffic_history catches ValueError before
// AgentDeploymentConflict, and AgentDeploymentConflict is a ValueError, so a
// conflict answers 400, never 409. Repository ValueErrors answer 400 too.
func TestSetTrafficHistoryErrorsFollowPythonExceptOrder(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want map[string]any
	}{
		{"value error", &agents.ValueError{Message: "流量记录只支持 mihomo Agent"}, map[string]any{"success": false, "message": "流量记录只支持 mihomo Agent"}},
		{"deployment conflict", &agents.DeploymentConflict{Message: "不能在配置事务中再次开启写事务"}, map[string]any{"success": false, "message": "不能在配置事务中再次开启写事务"}},
		{"repository in use", repo.NewInUse("路由器正在发布或同步，不能替换连接或删除记录，请稍后重试", nil), map[string]any{"success": false, "message": "路由器正在发布或同步，不能替换连接或删除记录，请稍后重试"}},
		{"repository validation", repo.NewValidationError("系统设置和备份设置必须是对象"), map[string]any{"success": false, "message": "系统设置和备份设置必须是对象"}},
	} {
		response := setTrafficHistoryError(c.err)
		if response == nil {
			t.Errorf("%s: no response, want 400 %v", c.name, c.want)
			continue
		}
		value, _ := response.Value()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if response.Status != 400 || !testjson.Equal(json.RawMessage(raw), c.want) {
			t.Errorf("%s: %d %s, want 400 %v", c.name, response.Status, raw, c.want)
		}
	}
	if response := setTrafficHistoryError(errors.New("disk I/O error")); response != nil {
		t.Errorf("other errors must reach the 500 page, got %+v", response)
	}
}
