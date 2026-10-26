// Package servernotes builds bounded, read-only reminders from server metadata.
// It neither installs an agent nor reads credentials, environments or TLS keys.
package servernotes

import "sshclient/internal/monitor"

const nginxDomains = `
 {line=$0; sub(/#.*/, "", line)
  if (!reading) {if (match(line,/^[ \t]*server_name[ \t]+/)) {line=substr(line,RLENGTH+1); reading=1} else next}
  done=index(line,";")>0; sub(/;.*/, "", line)
  n=split(line,values,/[ \t\r\n]+/)
  for(i=1;i<=n;i++) if(values[i]!="") printf "@@NOTE_DOMAIN\t%s\t%s\n", source, values[i]
  if(done) reading=0
 }`

const caddyDomains = `
 {line=$0; sub(/#.*/, "", line)
  if(depth==0 && index(line,"{")>0) {header=line; sub(/\{.*/,"",header); n=split(header,a,/[, \t]+/); for(i=1;i<=n;i++) if(a[i]!="") printf "@@NOTE_DOMAIN\t/etc/caddy/Caddyfile\t%s\n", a[i]}
  tmp=line; depth+=gsub(/\{/,"{",tmp); tmp=line; depth-=gsub(/\}/,"}",tmp); if(depth<0) depth=0
 }`

// One batched metadata query, only for an explicit notes scan. Read neither
// unit contents nor ExecStart/environment values, and emit at most 1024 rows.
const unitOrigins = `
 function emit() {if(id!="" && file!="" && ++count<=1024) printf "@@NOTE_UNIT\t%s\t%s\n", id, file; id=""; file=""}
 /^Id=/ {id=substr($0,4)}
 /^FragmentPath=/ {file=substr($0,14)}
 /^$/ {emit()}
 END {emit()}`

// The shell is fixed: no saved hostname, note, domain or container name is
// interpolated into it. Only selected metadata is returned, never full configs.
const Command = monitor.InventoryCommand + `
printf '\n@@NOTE_PROBE\n'
if command -v systemctl >/dev/null 2>&1; then
 systemctl show --property=Id,FragmentPath -- '*.service' 2>/dev/null | head -n 8192 | awk '` + unitOrigins + `'
fi
if command -v ss >/dev/null 2>&1; then
 ss -H -lntu 2>/dev/null | head -n 257 | awk '$1 ~ /^(tcp|udp)/ {printf "@@NOTE_PORT\t%s\t%s\n", substr($1,1,3), $5}'
elif command -v netstat >/dev/null 2>&1; then
 netstat -lntu 2>/dev/null | head -n 260 | awk '$1 ~ /^(tcp|udp)/ {printf "@@NOTE_PORT\t%s\t%s\n", substr($1,1,3), $4}'
else printf '@@NOTE_WARNING\t未提供可读取的监听端口接口\n'; fi
checked=0
for f in /etc/nginx/nginx.conf /etc/nginx/conf.d/*.conf /etc/nginx/sites-enabled/* /usr/local/openresty/nginx/conf/nginx.conf /usr/local/openresty/nginx/conf/conf.d/*.conf /opt/1panel/www/conf.d/*.conf /opt/1panel/apps/openresty/*/conf/nginx.conf /opt/1panel/apps/openresty/*/conf/conf.d/*.conf; do
 [ -f "$f" ] && [ -r "$f" ] || continue
 checked=$((checked + 1))
 if [ "$checked" -gt 128 ]; then printf '@@NOTE_WARNING\t站点配置超过 128 份，仅检测前 128 份\n'; break; fi
 head -c 262144 "$f" 2>/dev/null | awk -v source="$f" '` + nginxDomains + `'
done
if [ "$checked" -lt 128 ] && [ -r /etc/caddy/Caddyfile ]; then
 checked=$((checked + 1))
 head -c 262144 /etc/caddy/Caddyfile | awk '` + caddyDomains + `'
fi
if [ "$checked" = 0 ]; then printf '@@NOTE_WARNING\t未发现可读取的常见站点配置，无法据此列出网站域名\n'; fi
printf '@@NOTE_DONE\n'
`
