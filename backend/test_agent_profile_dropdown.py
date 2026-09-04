from pathlib import Path


def _agents_source() -> str:
    return (Path(__file__).resolve().parents[1] / "frontend" / "src" / "views" / "Agents.vue").read_text(encoding="utf-8")


def test_agent_profile_binding_uses_visible_select():
    """绑定配置空间的下拉必须可见、可读、且绑定到当前 profile。

    历史背景：这里原本用 el-select，其 popper 在深色主题下弹出浅色面板、
    iOS Safari 上文字被系统色覆盖，因此曾改为原生 <select> 并显式覆盖
    -webkit-text-fill-color 与 color-scheme。全站迁移到 shadcn 后，下拉面板
    是我们自己的 DOM、由设计系统 token 着色，两类问题都不会重现，
    因此断言改为「用 shadcn Select 且绑定正确」，仍然守住同一个意图。
    """
    source = _agents_source()
    marker = "绑定配置空间"
    section = source[source.index(marker):source.index(marker) + 900]

    assert "<Select" in section
    assert "<el-select" not in section
    # 仍然禁止回到原生 select：它的下拉面板由系统渲染，无法跟随主题
    assert "<select" not in section
    assert ":model-value=\"agent.profile_id || 'default'\"" in section
    assert '<SelectItem v-for="profile in profiles"' in section
    assert "{{ profile.name }}" in section
    # 绑定进行中要禁用，避免连点产生竞态
    assert ':disabled="bindingAgentId === agent.id"' in section


def test_agent_profile_change_forwards_selected_profile_id():
    """切换事件必须把选中的 profileId 交给 bindAgentProfile，
    且在绑定成功前不抢先改写 agent.profile_id —— 失败时 Select 会自动回到原值。"""
    source = _agents_source()
    start = source.index("const handleAgentProfileChange")
    handler = source[start:start + 400]

    assert "agent: Agent, profileId: string" in handler
    assert "await bindAgentProfile(agent, profileId)" in handler
    # 值没变时不发请求
    assert "profileId === (agent.profile_id || 'default')" in handler
    # 不得在这里乐观写入，绑定失败会导致界面与后端不一致
    assert "agent.profile_id = profileId" not in handler
    assert "agent-profile-select-popper" not in source


def test_agent_profile_options_bind_to_unwrapped_profiles_ref():
    """模板里访问 useProfileStore() 返回对象的 ref 属性不会自动解包，
    v-for 会遍历到 ref 自身的内部键，渲染出一批空白选项。"""
    source = _agents_source()

    assert 'const { profiles, refreshProfiles } = useProfileStore()' in source
    assert 'v-for="profile in profiles"' in source
    assert 'profileStore.profiles' not in source
