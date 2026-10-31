#!/bin/sh
set -eu

certificate_dir=/etc/letsencrypt/live/applefamily.shuangdeng.space
openresty_dir=/opt/1panel/apps/openresty/openresty/conf/ssl/applefamily.shuangdeng.space

install -d -m 755 "$openresty_dir"
install -o root -g root -m 644 "$certificate_dir/fullchain.pem" "$openresty_dir/fullchain.pem"
install -o root -g root -m 600 "$certificate_dir/privkey.pem" "$openresty_dir/privkey.pem"
docker exec 1Panel-openresty-NhRG openresty -t
docker exec 1Panel-openresty-NhRG openresty -s reload
