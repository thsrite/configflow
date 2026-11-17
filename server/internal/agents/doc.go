// Package agents is the Agent registry (package #20): the port of
// backend/agents/manager.py (AgentManager), common/agent_manager.py,
// runtime_state.py, diagnostics.py, operation_events.py, idle_rounds.py,
// presence_log.py, version.py, deployment_messages.py and
// upgrade_messages.py.
//
// # Manager
//
// Get(ctx) is get_agent_manager() (one Manager per repository), Init(ctx)
// init_agent_manager(), New(ctx, repository) AgentManager(repository). Every
// method takes the request ctx first. Records are *jsonx.Object; errors are
// *DeploymentConflict (AgentDeploymentConflict, 409), *PermissionError
// (re-registration without the token, 401), *ValueError, *repo.Error,
// *TransportError (requests.RequestException), *RemoteError (RuntimeError
// with a message for the user).
//
//   - Registry: RegisterAgent, GetAllAgents (status derived by the two-minute
//     rule), GetAgentByID (stored status), GetAgentByToken, Agents,
//     UpdateAgent, RebindProfileAgents, DeleteAgent, UpdateHeartbeat,
//     SetDomainDiscovery, SetTrafficHistory, UpdateAgents (_update_agents:
//     edit the Agent list inside one system transaction).
//   - Freshness: HeartbeatFresh, OnlineStatus, IsOnline, IsUsable.
//   - Deployments: BeginDeployment, RecordDeployment(RecordOptions),
//     FinishDeploymentPreparation, GetDeployment, ActivateDeployment,
//     RollbackDeployment, RecoverDeployment, DeploymentRequest
//     (_deployment_request), GetDeploymentDirect / ActivateDeploymentDirect
//     (the underscore versions for #29) (the publishing chain lives in deploy).
//   - Agent calls: Call (any request with the Bearer token), PushLegacyConfig,
//     RestartAgentService, GetAgentStatus, GetAgentLogSources, GetAgentLogs,
//     ClearAgentLog, UninstallAgent, UpdateAgentVersion, ValidateAgentLogPath,
//     Get/SetLoggingConfig, AgentJSONRequest, ProbeDomains, ReturnRouting*,
//     RegionTargets, RegionCheck, OxidnsHARequest and OxidnsHA*.
//
// # Agent HTTP client
//
// Every request to an Agent goes through Client (Do(ctx, Request) →
// *Response, error). HTTPClient is the production client (requests'
// connect/read timeouts, proxies from the environment); tests install a fake
// with SetDefaultClient or Manager.SetClient. Transport failures are
// *TransportError; texts embedded in user messages ("Connection error: ...",
// error_details) are Go's net/http error texts, not requests' (the only
// deliberate text difference).
//
// # Runtime state
//
// Heartbeats live in memory (RuntimeFor(dataDir), shared by every Manager of
// the data directory) and are laid over the stored records; only a changed
// version/config_version is written. ImportLegacyDatabase carries a #369
// runtime.sqlite3 into memory once and removes it.
//
// # Hooks for later packages
//
// RegisterHooks(Hooks{...}) from the owning package's wire function:
// AddMetrics / DeleteMetricsHistory / DeleteTrafficHistory /
// PublicationComplete / PublicationCheckpoint (#21), AddressFollow /
// AddressConfirmed / ReceiptMatches (#28), HAServiceOperation /
// HADeployment / HAActivate (#29),
// InstallationProfile / InstallationRegister (#30). OnAgentDeleted adds the
// stores the DELETE route clears (#21 traffic_store, #14 domain_probe and
// region_check, in that order); OnManagerInit adds the loops
// init_agent_manager starts (#28, #29, #31). SetRuleSyncRow (#27) and
// SetDNSFollowRow (#28) provide the rows of the "agent" task slot; the slot
// is installed when both are set (the registry owns no task rows).
// compat.go holds the pure oxidns_ha_publish helpers until #29 lands.
package agents
