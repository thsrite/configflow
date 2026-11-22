
# L1 D5: probe the Docker daemon's network namespace, then verify the core
# process rather than treating a running supervisor container as a healthy DNS.
cf_check_dns_port() {
  docker run --rm --network host --entrypoint configflow-agent "$1" ha-port-check || return 1
}

cf_wait_oxidns() {
  cf_health_id=$1
  case "$cf_health_id" in ''|*[!0-9a-f]*) printf '%s\n' '无法确认本次 OxiDNS 容器 ID，未确认安装成功。' >&2; return 1 ;; esac
  [ "${#cf_health_id}" -eq 64 ] || return 1
  cf_health_attempt=0
  while [ "$cf_health_attempt" -lt 30 ]; do
    cf_health_status=$(docker exec "$cf_health_id" supervisorctl -c /etc/supervisor/supervisord.conf status oxidns 2>/dev/null || true)
    case "$cf_health_status" in
      'oxidns '*RUNNING*) return 0 ;;
      'oxidns '*FATAL*|'oxidns '*EXITED*|'oxidns '*STOPPED*) printf '%s\n' 'OxiDNS 未正常运行，未确认安装成功；请检查容器日志和 53 端口。' >&2; return 1 ;;
    esac
    cf_health_attempt=$((cf_health_attempt + 1))
    sleep 1
  done
  printf '%s\n' '等待 OxiDNS 正常运行超时，未确认安装成功；请检查容器日志。' >&2
  return 1
}

