package routers

import (
	"encoding/json"

	"configflow/internal/routers/clientlist"
	"configflow/internal/routers/device"
	"configflow/internal/routers/dnsguard"
	"configflow/internal/routers/fixedip"
	"configflow/internal/routers/ikuai"
	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/rospolicy"
	"configflow/internal/routers/routeedit"
	"configflow/internal/routers/spec"
)

// Temporary type and constant aliases for R1–R9; removed in R10.
type (
	ErrorKind            = device.ErrorKind
	RouterError          = device.RouterError
	ValidationError      = device.ValidationError
	DeviceHTTP           = device.DeviceHTTP
	RouterInfo           = device.RouterInfo
	RouteEntry           = device.RouteEntry
	PolicyRouteEntry     = device.PolicyRouteEntry
	NatBypassEntry       = device.NatBypassEntry
	DhcpDnsEntry         = device.DhcpDnsEntry
	DhcpDnsState         = device.DhcpDnsState
	ReportPart           = device.ReportPart
	CIDRGateway          = device.CIDRGateway
	NATChecks            = device.NATChecks
	Op                   = device.Op
	OpHead               = device.OpHead
	Stage                = device.Stage
	NATUpdateOp          = device.NATUpdateOp
	StaticRouteSelection = device.StaticRouteSelection
	StaticRouteIndex     = device.StaticRouteIndex
	DNSAgent             = device.DNSAgent
	AgentHA              = device.AgentHA
	DNSHA                = device.DNSHA
	DNSSettings          = device.DNSSettings
	DNSReportItem        = device.DNSReportItem
	DNSReport            = device.DNSReport
	DNSCheck             = device.DNSCheck
	SupernetIndex        = device.SupernetIndex
	ForwardHintResult    = device.ForwardHintResult
	RouteItem            = device.RouteItem
	ExtraRoute           = device.ExtraRoute
	UnmanagedRoute       = device.UnmanagedRoute
	RetiredPolicyItem    = device.RetiredPolicyItem
	PolicyRules          = device.PolicyRules
	RoutesReport         = device.RoutesReport
	NatItem              = device.NatItem
	DisabledNat          = device.DisabledNat
	NatReport            = device.NatReport

	Row              = ros.Row
	NatContext       = ros.NatContext
	NatAssessment    = ros.NatAssessment
	RouteEgressIndex = ros.RouteEgressIndex
	BypassExpansion  = ros.BypassExpansion
	NATListRule      = ros.NATListRule
	ListRef          = ros.ListRef
	ROSChange        = ros.ROSChange
	ROSUndo          = ros.ROSUndo
	ROSIDs           = ros.ROSIDs
	NATStep          = ros.NATStep
	ROSResource      = ros.Resource
	Manifest         = ros.Manifest
	Manifests        = ros.Manifests
	ManifestHolder   = ros.ManifestHolder

	GroupEntry      = ikuai.GroupEntry
	GroupForm       = ikuai.GroupForm
	GroupMigration  = ikuai.GroupMigration
	GroupOp         = ikuai.GroupOp
	GroupPlanner    = ikuai.GroupPlanner
	GroupReport     = ikuai.GroupReport
	GroupRule       = ikuai.GroupRule
	GroupRuleCheck  = ikuai.GroupRuleCheck
	GroupStageCheck = ikuai.GroupStageCheck
	GroupState      = ikuai.GroupState
	GroupUser       = ikuai.GroupUser
	IkuaiGroup      = ikuai.IkuaiGroup

	ContentLoader    = spec.ContentLoader
	DNSGuard         = spec.DNSGuard
	Desired          = spec.Desired
	DesiredDNSGuard  = spec.DesiredDNSGuard
	LibraryRule      = spec.LibraryRule
	PDGuard          = spec.PDGuard
	ProxyCIDR        = spec.ProxyCIDR
	ROSPolicyDesired = spec.ROSPolicyDesired
	Router           = spec.Router
	RouterSettings   = spec.RouterSettings
	RouterView       = spec.RouterView
	RuleSet          = spec.RuleSet

	EditableRoute = routeedit.EditableRoute
	RouteSnapshot = routeedit.RouteSnapshot
	RouteFields   = routeedit.RouteFields
	PublicRoute   = routeedit.PublicRoute
	RouteValues   = routeedit.RouteValues
	RouteIntent   = routeedit.RouteIntent
	RoutePlan     = routeedit.RoutePlan
	PlannedRoute  = routeedit.PlannedRoute
	RouteMutation = routeedit.RouteMutation
	RouteResult   = routeedit.RouteResult
	RouteEditor   = routeedit.RouteEditor

	DNSGuardSnapshot = dnsguard.Snapshot
	DNSGuardSpec     = dnsguard.Spec
	DNSGuardResource = dnsguard.Resource
	DNSGuardManifest = dnsguard.Manifest
	DNSGuardCreate   = dnsguard.Create
	DNSGuardReplace  = dnsguard.Replace
	DNSGuardUndo     = dnsguard.Undo
	DNSGuardItem     = dnsguard.Item
	DNSGuardReport   = dnsguard.Report
	DNSGuardPlan     = dnsguard.Plan
	DNSGuardCheck    = dnsguard.Check

	FixedIPBinding   = fixedip.Binding
	FixedIPUndo      = fixedip.Undo
	FixedIPOperation = fixedip.Operation
	FixedIPSnapshot  = fixedip.Snapshot
	FixedIPState     = fixedip.State
	FixedIPDriver    = fixedip.Device

	ClientReadOptions = clientlist.ReadOptions
	ClientReader      = clientlist.Reader
	ConnectionQuery   = clientlist.ConnectionQuery

	NATBaseline       = rospolicy.NATBaseline
	NATListCheck      = rospolicy.NATListCheck
	NATListEvidence   = rospolicy.NATListEvidence
	NATListMembers    = rospolicy.NATListMembers
	NATListPlan       = rospolicy.NATListPlan
	NATListSync       = rospolicy.NATListSync
	NATListUndo       = rospolicy.NATListUndo
	NATLists          = rospolicy.NATLists
	ROSPolicyCheck    = rospolicy.ROSPolicyCheck
	ROSPolicyItem     = rospolicy.ROSPolicyItem
	ROSPolicyManifest = rospolicy.ROSPolicyManifest
	ROSPolicyObject   = rospolicy.ROSPolicyObject
	ROSPolicyPlan     = rospolicy.ROSPolicyPlan
	ROSPolicyReplace  = rospolicy.ROSPolicyReplace
	ROSPolicySnapshot = rospolicy.ROSPolicySnapshot
	ROSPolicySpec     = rospolicy.ROSPolicySpec
	ROSPolicyUndo     = rospolicy.ROSPolicyUndo
	ROSReplacement    = rospolicy.ROSReplacement

	ForwardInputs              = ipv6.ForwardInputs
	ForwardHintCheck           = ipv6.ForwardHintCheck
	IPv6DNSCheck               = ipv6.IPv6DNSCheck
	IPv6DNSEvidence            = ipv6.IPv6DNSEvidence
	IPv6DNSItem                = ipv6.IPv6DNSItem
	IPv6DNSPlan                = ipv6.IPv6DNSPlan
	IPv6DNSReport              = ipv6.IPv6DNSReport
	IPv6DNSUpdate              = ipv6.IPv6DNSUpdate
	IPv6EvidenceSet            = ipv6.IPv6EvidenceSet
	IPv6FakeIPCheck            = ipv6.IPv6FakeIPCheck
	IPv6FakeIPManifest         = ipv6.IPv6FakeIPManifest
	IPv6FakeIPPlan             = ipv6.IPv6FakeIPPlan
	IPv6FakeIPReplace          = ipv6.IPv6FakeIPReplace
	IPv6FakeIPReport           = ipv6.IPv6FakeIPReport
	IPv6FakeIPUndo             = ipv6.IPv6FakeIPUndo
	IPv6Host                   = ipv6.IPv6Host
	IPv6HostAddress            = ipv6.IPv6HostAddress
	IPv6PDEvidence             = ipv6.IPv6PDEvidence
	IPv6PDGuardCheck           = ipv6.IPv6PDGuardCheck
	IPv6PDGuardPlan            = ipv6.IPv6PDGuardPlan
	IPv6PDGuardReport          = ipv6.IPv6PDGuardReport
	IPv6PDGuardSync            = ipv6.IPv6PDGuardSync
	IPv6PDGuardUndo            = ipv6.IPv6PDGuardUndo
	IPv6PDItem                 = ipv6.IPv6PDItem
	IPv6PDManifest             = ipv6.IPv6PDManifest
	IPv6Readiness              = ipv6.IPv6Readiness
	IPv6ReadinessCheck         = ipv6.IPv6ReadinessCheck
	IPv6ReadinessConfiguration = ipv6.IPv6ReadinessConfiguration
	IPv6ReadinessReceipt       = ipv6.IPv6ReadinessReceipt
	IPv6RouteItem              = ipv6.IPv6RouteItem
	IPv6RouteSettings          = ipv6.IPv6RouteSettings
	IPv6RouteSpec              = ipv6.IPv6RouteSpec
	IPv6RoutesCheck            = ipv6.IPv6RoutesCheck
	IPv6RoutesManifest         = ipv6.IPv6RoutesManifest
	IPv6RoutesPlan             = ipv6.IPv6RoutesPlan
	IPv6RoutesReplace          = ipv6.IPv6RoutesReplace
	IPv6RoutesReport           = ipv6.IPv6RoutesReport
	IPv6RoutesUndo             = ipv6.IPv6RoutesUndo
	IPv6Settings               = ipv6.IPv6Settings
	IPv6Snapshot               = ipv6.IPv6Snapshot
	PolicyClient               = ipv6.PolicyClient
	PolicyConfiguration        = ipv6.PolicyConfiguration
	PolicyContext              = ipv6.PolicyContext
	PolicyNodeTarget           = ipv6.PolicyNodeTarget
	PolicyPacket               = ipv6.PolicyPacket
	PolicySnapshot             = ipv6.PolicySnapshot
)

// FromConfig returns owned router records; temporary alias removed in R10.
func FromConfig(config json.RawMessage) []*Router { return spec.RoutersOf(config) }

const (
	KindRouter           = device.KindRouter
	KindConflict         = device.KindConflict
	KindRollback         = device.KindRollback
	KindUncertainWrite   = device.KindUncertainWrite
	KindMissingResource  = device.KindMissingResource
	KindPingNotPermitted = device.KindPingNotPermitted
	KindRejected         = device.KindRejected
	KindInvalidResponse  = device.KindInvalidResponse

	ManagedPrefix       = device.ManagedPrefix
	PolicyAutoComment   = device.PolicyAutoComment
	PolicyManualComment = device.PolicyManualComment
	RestoredComment     = device.RestoredComment
	PolicyPriority      = device.PolicyPriority

	StageReturnRouting = device.StageReturnRouting
	StageRoutes        = device.StageRoutes
	StageIPv6FakeIP    = device.StageIPv6FakeIP
	StageIPv6Routes    = device.StageIPv6Routes
	StageNATBypass     = device.StageNATBypass
	StageDHCPDNS       = device.StageDHCPDNS
	StageIPv6DNS       = device.StageIPv6DNS
	StageDNSGuard      = device.StageDNSGuard
	StageIPv6PDGuard   = device.StageIPv6PDGuard
	StageReturnCleanup = device.StageReturnCleanup

	NATListComment = ros.NATListComment
	NATSrcList     = ros.NATSrcList
	NATDstList     = ros.NATDstList
	PathNAT        = ros.PathNAT
	PathMangle     = ros.PathMangle

	GroupMaxBytes   = ikuai.GroupMaxBytes
	GroupMaxEntries = ikuai.GroupMaxEntries
	MaxRuleGroups   = ikuai.MaxRuleGroups
	OpGroupAdd      = ikuai.OpGroupAdd
	OpGroupDelete   = ikuai.OpGroupDelete
	OpGroupUpdate   = ikuai.OpGroupUpdate
	PolicyNexthop   = ikuai.PolicyNexthop
	RefForeign      = ikuai.RefForeign
	RefOwned        = ikuai.RefOwned
	RefStale        = ikuai.RefStale

	DNSGuardBypassList = dnsguard.BypassList
)
