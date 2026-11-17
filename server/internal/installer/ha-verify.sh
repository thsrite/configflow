cf_dns_vip={{VIP}}
if command -v dig >/dev/null 2>&1; then
  dig +time=3 +tries=1 "@$cf_dns_vip" example.com
elif command -v nslookup >/dev/null 2>&1; then
  nslookup example.com "$cf_dns_vip"
elif command -v busybox >/dev/null 2>&1 && busybox --list | grep -qx nslookup; then
  busybox nslookup example.com "$cf_dns_vip"
else
  printf '%s\n' '未找到可查询指定 DNS 服务器的工具。Debian/Ubuntu 请安装 dnsutils，Alpine 请安装 bind-tools，然后重新执行验证命令。' >&2
  exit 127
fi
