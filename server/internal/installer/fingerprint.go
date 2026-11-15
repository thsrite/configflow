package installer

// GeneratorFingerprint is generator_fingerprint() of the Python release this
// package reproduces: sha256(LATEST_AGENT_VERSION, then name NUL bytes of
// backend/agents/scripts/*.sh, backend/agents/*install*.py except
// install_command_store.py, and docker_ha_guide.py, in sorted path order),
// first 32 hex digits. A link saved under another value is served as
// StaleScript, so links made by the same release stay valid whether Python
// or Go made them, and links made before an update are refused.
//
// TestGeneratorFingerprintMatchesPython recomputes it from backend/agents and
// agents.LatestAgentVersion and fails when a script, generator or the Agent
// version changed without updating this value.
const GeneratorFingerprint = "4c01744e968c8fa791638b5943d5a61c"
