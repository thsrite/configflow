package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/dnsguard"
	"configflow/internal/routers/ikuai"
	"configflow/internal/routers/ros"
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
)

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
