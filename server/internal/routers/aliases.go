package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
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
)
