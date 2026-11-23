package routers

import "configflow/internal/routers/ipv6"

const (
	rosIPv4Addresses          = ipv6.PathIPv4Addresses
	rosIPv6Addresses          = ipv6.PathIPv6Addresses
	pdScripts                 = ipv6.PathPDScripts
	pdSchedulers              = ipv6.PathPDSchedulers
	pdClients                 = ipv6.PathPDClients
	pdPrefixes                = ipv6.PathPDPrefixes
	pdLast                    = ipv6.PDLastList
	pdWatch                   = ipv6.PDWatchScript
	pdPoll                    = ipv6.PDPollScheduler
	pdPolicy                  = ipv6.PDPolicy
	pdEvent                   = ipv6.PDEvent
	pdHook                    = ipv6.PDHook
	pdRollback                = ipv6.PDRollback
	messageIPv6PreviewChanged = ipv6.MessagePreviewChanged
	messageIPv6FakeSettings   = ipv6.MessageFakeSettings
	messageIPv6Range          = ipv6.MessageRange
	messageForwardIPv4Drop    = ipv6.MessageForwardIPv4
	PolicyUnknown             = ipv6.PolicyUnknown
	PolicyNoMatch             = ipv6.PolicyNoMatch
)
