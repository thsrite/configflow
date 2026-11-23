package ipv6

const (
	messageForwardIPv4Drop    = "IPv4 防火墙会丢弃同口转发的后续包：设备经 ROS 同一 LAN 口转给 Mihomo，回包不经过 ROS，ROS 把后续包判为 invalid，被 forward 链丢弃（如出厂的「drop invalid」），新连接可能卡几秒甚至超时。ROS 的 ICMP 重定向只能部分缓解：有数量限制，同时打开多个连接时仍会卡，开了转发的 Linux 设备还会忽略，不能依赖。执行下方命令放行同口转发后，连接即恢复正常；ConfigFlow 不修改防火墙，路由照常发布。"
	messageForwardIPv6Drop    = "IPv6 防火墙会丢弃同口转发的后续包：设备经 ROS 同一 LAN 口转给 Mihomo，回包不经过 ROS，ROS 把后续包判为 invalid，被 forward 链丢弃（如出厂的「drop invalid」），新连接可能卡几秒甚至超时。ROS 的 ICMPv6 重定向只能部分缓解：有数量限制，同时打开多个连接时仍会卡，开了转发的 Linux 设备还会忽略，不能依赖。执行下方命令放行同口转发后，连接即恢复正常；一条命令同时覆盖 IPv6 Fake-IP 和 IPv6 规则集路由；ConfigFlow 不修改防火墙，路由照常发布。"
	messageForwardUnknownLAN  = "未能确认 IPv4 防火墙是否会丢弃转给 Mihomo 的同口转发：无法确定 Mihomo 所在的 LAN 口。"
	messageForwardUnknownRead = "未能确认 IPv4 防火墙是否会丢弃转给 Mihomo 的同口转发：无法读取 RouterOS IPv4 防火墙。"
)
