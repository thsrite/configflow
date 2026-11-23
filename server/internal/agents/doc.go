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
// method takes the request ctx first. Records are native model.Agent/model.Deployment values; errors are
// *DeploymentConflict (AgentDeploymentConflict, 409), *PermissionError
// (re-registration without the token, 401), *ValidationError, *repo.Error,
// *TransportError (requests.RequestException), *RemoteError (RuntimeError
// with a message for the user).
//
//   - Registry: RegisterIdentity, ReadAgent/ReadAgents/ListAgents, ReadAgentByToken,
//     EditAgents, RebindProfiles, DeleteAgentContext, ObserveHeartbeat and the
//     model-based management switches. Snapshots are caller-owned.
//   - Deployments: ReserveDeployment, RecordDeploymentModel, GetDeploymentModel,
//     ActivateDeploymentModel, RollbackDeploymentModel, RecoverDeploymentModel,
//     DeploymentRequestModel; preparation leases are always explicitly released.
//   - Agent calls: CallAgent, RemoteCall, AgentRequestModel, FeatureRequestModel,
//     ProbeDomainsModel, RegionTargetsModel, RegionCheckModel and ReturnRouting*Model.
//     Compatibility methods still used by deployment/gateway owners are removed by N8-11 W9.
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
// AddMetricsJSON / DeleteMetricsHistoryContext / DeleteTrafficHistoryContext,
// PublicationComplete / PublicationCheckpoint, AddressFollowTyped /
// AddressConfirmedTyped, ReceiptMatches, HAServiceOperationTyped / HADeployment /
// HAActivate, InstallationProfileTyped / InstallationRegisterTyped. Legacy-only
// address, installation and HA-operation hook fields no longer exist; registering
// an obsolete hook is a compile error. RegisterHooks replaces each supplied field
// while leaving unrelated owners installed; ResetHooks clears all owners for tests.
// OnAgentDeleted registers the per-Agent leaf stores to clear. OnManagerInit adds
// lifecycle-managed initialization. SetRuleSyncRow / SetDNSFollowRow take native
// Agent snapshots and task receipts and supply the shared Agent task slot.
package agents
