from pathlib import Path


def _agents_source() -> str:
    return (Path(__file__).resolve().parents[1] / "frontend" / "src" / "views" / "Agents.vue").read_text(encoding="utf-8")


def test_agent_profile_binding_uses_visible_native_select():
    source = _agents_source()
    # 文案统一为「配置空间」后，锚点随之更新（原为「配置 Profile」）
    marker = "绑定配置空间"
    section = source[source.index(marker):source.index(marker) + 900]

    assert "<select" in section
    assert 'class="agent-profile-native-select"' in section
    assert ':value="agent.profile_id || \'default\'"' in section
    assert '@change="handleAgentProfileChange(agent, $event)"' in section
    assert "<el-select" not in section
    assert "<option" in section
    assert "profile.name" in section
    assert ".agent-profile-native-select {" in source
    # 原生 select 的文字色必须显式覆盖（Safari / iOS 否则用系统色），
    # 且取自设计系统 token 而非写死色值，才能同时适配深浅主题
    assert "-webkit-text-fill-color: var(--cf-fg);" in source
    # 不得锁定 color-scheme：锁 light 会让原生下拉在深色主题下弹出浅色面板
    assert "color-scheme: inherit;" in source
    assert "color-scheme: light;" not in source


def test_agent_profile_native_change_forwards_selected_profile_id():
    source = _agents_source()
    start = source.index("const handleAgentProfileChange")
    handler = source[start:start + 400]

    assert "event.target" in handler
    assert "select instanceof HTMLSelectElement" in handler
    assert "const previousProfileId = agent.profile_id || 'default'" in handler
    assert "await bindAgentProfile(agent, select.value)" in handler
    assert "select.value = previousProfileId" in handler
    assert "agent-profile-select-popper" not in source


def test_agent_profile_options_bind_to_unwrapped_profiles_ref():
    """模板里访问 useProfileStore() 返回对象的 ref 属性不会自动解包，
    v-for 会遍历到 ref 自身的内部键，渲染出一批空白 <option>。"""
    source = _agents_source()

    assert 'const { profiles, refreshProfiles } = useProfileStore()' in source
    assert 'v-for="profile in profiles"' in source
    assert 'profileStore.profiles' not in source
