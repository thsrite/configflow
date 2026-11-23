F=open('server/design/o17-wiring/routers_files.txt').read().split()
PKG={
 'device':(0,['model.go','plan_model.go','staticindex.go','dhcpdns.go','messages.go']),
 'ros':(1,['routeros.go','routeros_batch.go','rosnat.go','rosvalues.go','ros_script.go','natlist.go','addresslists.go','addresswrite.go','device_redact.go']),
 'ikuai':(1,['ikuai.go','ikuai_apply.go','ikuai_dhcp.go','ikuai_forms.go','ikuai_row.go','ikuaigroups.go']),
 'spec':(1,['router.go','stored_model.go','desired.go']),
 'oplock':(2,['joblock.go','operation_group.go']),
 'routeedit':(2,['route_editor.go','routeros_routes.go','ikuai_routes.go']),
 'natlist':(2,['natlist_plan.go','natlist_read.go','natlist_write.go']),
 'rospolicy':(3,['rospolicy_model.go','rospolicy_plan.go','rospolicy_read.go','rospolicy_restore.go','rospolicy_write.go']),
 'dnsguard':(2,['dns_guard_messages.go','dns_guard_model.go','dns_guard_plan.go','dns_guard_spec.go','dns_guard_write.go']),
 'ipv6':(2,[f for f in F if f.startswith('ipv6_')]+['forward_hint.go','forward_hint_messages.go']),
 'fixedip':(2,['fixed_ip_ikuai_read.go','fixed_ip_ikuai_write.go','fixed_ip_messages.go','fixed_ip_model.go','fixed_ip_read.go','fixed_ip_ros_write.go']),
 'clientlist':(2,['client_messages.go','clients_ikuai.go','clients_reader.go','clients_ros.go','connection_reader.go']),
 'routers':(4,['plan.go','plan_evidence.go','plan_messages.go','plan_nat.go','plan_return.go','plan_routes.go','release.go','release_messages.go','release_model.go','publish.go','report.go','inspect.go','pending.go','pending_cache.go','pending_messages.go','pending_summary.go','management.go','management_messages.go','management_repo.go','workspace.go','repository_hooks.go','wire.go','agent_return.go','return_advisory.go']),
 'rulesync':(5,[f for f in F if f.startswith('rule_sync')]),
}
# symbol relocations (resolution of back edges)
MOVE={}
for s in 'uniqueStrings firstNonEmpty nonEmpty stringSet jsonEqual jsonValuesEqual mustPolicyJSON orText requestString hasStoredState Networks SupernetIndex (*SupernetIndex).Covers NewSupernetIndex ReportPart DeviceHTTP (DeviceHTTP).Do (DeviceHTTP).Close isRouterError routerErrorKind rollbackError'.split(): MOVE[s]='device'
for s in 'ROSResource resource rosTables StableIPv6Row policyFlag messageIPv6RoutesOwnership ipv6DNSFlag rosIPv6ND pdLists guardRaw'.split(): MOVE[s]='ros'
for s in 'DesiredDNSGuard ROSPolicyDesired pendingProfile ikuaiDHCPStatic'.split(): MOVE[s]='spec'
for s in 'RouterState Driver PolicyRoute RetirePolicyRoutes StaleRoutes StaleRouteWarning NewDriver (*RouterOS).ReadState (*RouterOS).stateNATLists (*Ikuai).ReadState'.split(): MOVE[s]='routers'
for s in 'RouterOperationLock'.split(): MOVE[s]='oplock'
for s in 'findSyncRouter'.split(): MOVE[s]='routers'
for s in 'Report NatItem'.split(): MOVE[s]='routers'
MSGPKG={}
ALLOWED={'device':set(),'ros':{'device'},'ikuai':{'device'},'spec':{'device'},
 'oplock':{'device','spec'},
 'routeedit':{'device','ros','ikuai','spec','oplock'},
 'natlist':{'device','ros','spec'},'dnsguard':{'device','ros','spec'},'ipv6':{'device','ros','spec','dnsguard'},
 'rospolicy':{'device','ros','spec','natlist'},
 'fixedip':{'device','ros','ikuai','spec'},'clientlist':{'device','ros','ikuai','spec'},
 'routers':{'device','ros','ikuai','spec','oplock','routeedit','natlist','rospolicy','dnsguard','ipv6','fixedip','clientlist'},
}
ALLOWED['rulesync']=set(ALLOWED['routers'])|{'routers'}
PKG['rospolicy'][1].extend(['natlist_plan.go','natlist_read.go','natlist_write.go']); del PKG['natlist']
PKG['ipv6']=(4,PKG['ipv6'][1]); PKG['routers']=(5,PKG['routers'][1]); PKG['rulesync']=(6,PKG['rulesync'][1])
for s in 'findSyncRouter routersOf releaseRouterRecord pendingProfile messageIPv6YAMLType'.split(): MOVE[s]='spec'
for s in 'rosError routeLocks'.split(): MOVE[s]={'rosError':'device','routeLocks':'oplock'}[s]
for s in 'NatItem'.split(): MOVE[s]='device'
ALLOWED={'device':set(),'ros':{'device'},'ikuai':{'device'},'spec':{'device'},
 'oplock':{'device','spec'},
 'routeedit':{'device','ros','ikuai','spec','oplock'},
 'dnsguard':{'device','ros','spec'},
 'rospolicy':{'device','ros','spec','routeedit'},
 'ipv6':{'device','ros','spec','dnsguard','rospolicy'},
 'fixedip':{'device','ros','ikuai','spec'},'clientlist':{'device','ros','ikuai','spec'},
}
ALLOWED['routers']=set(PKG)-{'routers','rulesync'}
ALLOWED['rulesync']=set(PKG)-{'rulesync'}
