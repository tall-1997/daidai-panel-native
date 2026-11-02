#!/bin/sh
# 容器健康检查（#162）：跟着 PANEL_HOST 走。全部网卡时探 127.0.0.1，IPv6 加方括号；
# 非法值进不到这里（entrypoint 启动时就报错退出了）。
host=${PANEL_HOST:-}
host=${host#\[}
host=${host%\]}
case "${host}" in
  ''|0.0.0.0|::|localhost) host=127.0.0.1 ;;
  *:*) host="[${host}]" ;;
esac
exec curl --fail --silent --output /dev/null "http://${host}:${PANEL_PORT:-5700}/api/v1/health"
