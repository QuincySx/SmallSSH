# 常见应用服务识别清单

当前维护 100 项应用、162 个候选别名。清单用于识别已经读到的服务和容器，不是装机量排名；出现在清单里不表示服务器安装了它。

服务器备注默认仍只有「服务」和「域名」。以下分类用于维护规则，不会全部铺到界面上。

## 识别方式

- 在本地比对已有扫描结果，不增加 SSH、DNS、HTTP 或外部模型请求，不执行每个应用的版本命令。
- 使用明确别名、完整匹配的版本变体，以及模板实例；避免把 `redis-exporter` 当成 Redis、把 `gssproxy` 当成业务应用。
- 保留实例名，例如 `gunicorn@billing.service` 显示 Gunicorn（billing），`pm2-root.service` 显示 PM2（root）。
- 同一个 Python、Java、Node.js 或 Ruby 基础镜像承载的不同容器保留各自名字，不合并成一个无法区分的运行时。
- 未识别的自定义应用保留其服务名；其他系统组件放进检测详情。任意命名的 `myapp.service` 无法仅凭名字判断是 Spring Boot、Gin、Echo 还是其他框架。
- PM2、Supervisor 等管理器也能启动多种语言的程序。识别管理器不表示已经知道其所有子应用。

## 分类统计

| 分类 | 项数 | 应用 |
| --- | ---: | --- |
| Web | 8 | Nginx、OpenResty、Apache、Caddy、Traefik、HAProxy、Lighttpd、Varnish |
| Java | 8 | Java、Tomcat、Jetty、Spring Boot、WildFly、GlassFish、Jenkins、Keycloak |
| Node.js / JS | 13 | Node.js、PM2、Bun、Deno、Next.js、Nuxt、Express、NestJS、Fastify、Node-RED、n8n、Uptime Kuma、code-server |
| Python | 15 | Python、Gunicorn、uWSGI、Uvicorn、Daphne、Hypercorn、Celery、RQ、Flower、Django、Flask、FastAPI、Jupyter、Supervisor、Home Assistant |
| Ruby | 6 | Ruby、Puma、Unicorn、Passenger、Sidekiq、Rails |
| PHP | 6 | PHP、PHP-FPM、FrankenPHP、RoadRunner、Swoole、Laravel Octane |
| .NET | 1 | .NET / ASP.NET Core |
| 数据库 / 缓存 | 9 | MySQL、MariaDB、PostgreSQL、Redis、Valkey、MongoDB、Memcached、ClickHouse、InfluxDB |
| 消息 / 搜索 | 8 | RabbitMQ、Kafka、ActiveMQ、NATS、Mosquitto、Redpanda、Elasticsearch、OpenSearch |
| Go 常见应用 | 3 | etcd、Consul、Vault |
| 监控 | 5 | Prometheus、Grafana、Loki、Alertmanager、VictoriaMetrics |
| 网络 / 穿透 | 5 | FRP 服务端、FRP 客户端、Cloudflare Tunnel、Tailscale、ZeroTier |
| 常见自托管应用 | 13 | MinIO、Syncthing、Gitea、Forgejo、Vaultwarden、Nextcloud、Immich、Jellyfin、Alist、File Browser、Portainer、1Panel、Sub-Store |

Go 编写的常见产品也分布在 Web、监控、存储和穿透等分类中，例如 Caddy、Traefik、FrankenPHP、RoadRunner、NATS、etcd、Consul、Vault、Prometheus、Grafana、Loki、Alertmanager、VictoriaMetrics、FRP 服务端、FRP 客户端、Cloudflare Tunnel、Tailscale、MinIO、Syncthing、Gitea、Forgejo、Alist、File Browser、Portainer、1Panel。自编译 Go 应用通常使用自己的可执行文件名，不能据此假定具体框架。

## 规则及官方参考

规则数据位于 [catalog.json](../internal/servernotes/catalog.json)，匹配器位于 [catalog.go](../internal/servernotes/catalog.go)。每项保留官方项目或部署文档来源；候选服务别名由本项目维护，不承诺上游发行版一定使用这个单元名。添加规则后需通过别名冲突、误匹配和实例区分测试。

| 应用 | 分类 | 候选别名 | 部署 / 项目参考 |
| --- | --- | --- | --- |
| Nginx | Web | `nginx`, `nginx-debug` | [官方参考](https://nginx.org/en/) |
| OpenResty | Web | `openresty` | [官方参考](https://openresty.org/en/) |
| Apache | Web | `apache2`, `httpd`, `apache` | [官方参考](https://httpd.apache.org/docs/) |
| Caddy | Web | `caddy` | [官方参考](https://caddyserver.com/docs/running) |
| Traefik | Web | `traefik` | [官方参考](https://doc.traefik.io/traefik/) |
| HAProxy | Web | `haproxy` | [官方参考](https://docs.haproxy.org/) |
| Lighttpd | Web | `lighttpd` | [官方参考](https://www.lighttpd.net/) |
| Varnish | Web | `varnish`, `varnish-cache`, `varnishd` | [官方参考](https://varnish-cache.org/docs/) |
| Java | Java | `java`, `openjdk`, `eclipse-temurin`, `amazoncorretto`, `ibm-semeru-runtimes` | [官方参考](https://docs.spring.io/spring-boot/how-to/deployment/installing.html) |
| Tomcat | Java | `tomcat` | [官方参考](https://tomcat.apache.org/) |
| Jetty | Java | `jetty` | [官方参考](https://jetty.org/docs/) |
| Spring Boot | Java | `springboot`, `spring-boot` | [官方参考](https://docs.spring.io/spring-boot/how-to/deployment/installing.html) |
| WildFly | Java | `wildfly`, `jboss` | [官方参考](https://docs.wildfly.org/) |
| GlassFish | Java | `glassfish` | [官方参考](https://glassfish.org/) |
| Jenkins | Java | `jenkins` | [官方参考](https://www.jenkins.io/doc/book/installing/linux/) |
| Keycloak | Java | `keycloak` | [官方参考](https://www.keycloak.org/server/configuration) |
| Node.js | Node.js / JS | `node`, `nodejs` | [官方参考](https://nodejs.org/en/learn) |
| PM2 | Node.js / JS | `pm2` | [官方参考](https://pm2.keymetrics.io/docs/usage/startup/) |
| Bun | Node.js / JS | `bun` | [官方参考](https://bun.sh/docs) |
| Deno | Node.js / JS | `deno` | [官方参考](https://docs.deno.com/runtime/) |
| Next.js | Node.js / JS | `nextjs`, `next-js` | [官方参考](https://nextjs.org/docs/app/guides/self-hosting) |
| Nuxt | Node.js / JS | `nuxt`, `nuxtjs` | [官方参考](https://nuxt.com/docs/getting-started/deployment) |
| Express | Node.js / JS | `express`, `expressjs` | [官方参考](https://expressjs.com/en/advanced/best-practice-performance.html) |
| NestJS | Node.js / JS | `nestjs`, `nestjs-server` | [官方参考](https://docs.nestjs.com/deployment) |
| Fastify | Node.js / JS | `fastify` | [官方参考](https://fastify.dev/docs/latest/) |
| Node-RED | Node.js / JS | `node-red`, `nodered` | [官方参考](https://nodered.org/docs/getting-started/) |
| n8n | Node.js / JS | `n8n` | [官方参考](https://docs.n8n.io/hosting/) |
| Uptime Kuma | Node.js / JS | `uptime-kuma` | [官方参考](https://github.com/louislam/uptime-kuma) |
| code-server | Node.js / JS | `code-server` | [官方参考](https://coder.com/docs/code-server/install) |
| Python | Python | `python`, `python2`, `python3`, `pypy`, `pypy3` | [官方参考](https://docs.python.org/3/using/) |
| Gunicorn | Python | `gunicorn` | [官方参考](https://gunicorn.org/deploy/) |
| uWSGI | Python | `uwsgi` | [官方参考](https://uwsgi-docs.readthedocs.io/en/latest/Systemd.html) |
| Uvicorn | Python | `uvicorn` | [官方参考](https://uvicorn.dev/deployment/) |
| Daphne | Python | `daphne` | [官方参考](https://github.com/django/daphne) |
| Hypercorn | Python | `hypercorn` | [官方参考](https://hypercorn.readthedocs.io/en/latest/) |
| Celery | Python | `celery`, `celeryd`, `celerybeat`, `celery-beat` | [官方参考](https://docs.celeryq.dev/en/stable/userguide/daemonizing.html) |
| RQ | Python | `rq`, `rqworker`, `rq-worker` | [官方参考](https://python-rq.org/docs/workers/) |
| Flower | Python | `flower`, `celery-flower` | [官方参考](https://flower.readthedocs.io/en/latest/) |
| Django | Python | `django` | [官方参考](https://docs.djangoproject.com/en/stable/howto/deployment/) |
| Flask | Python | `flask` | [官方参考](https://flask.palletsprojects.com/en/stable/deploying/) |
| FastAPI | Python | `fastapi` | [官方参考](https://fastapi.tiangolo.com/deployment/) |
| Jupyter | Python | `jupyter`, `jupyter-lab`, `jupyterlab`, `jupyter-notebook`, `jupyterhub` | [官方参考](https://jupyter-server.readthedocs.io/en/latest/operators/) |
| Supervisor | Python | `supervisor`, `supervisord` | [官方参考](https://supervisord.org/running.html) |
| Home Assistant | Python | `home-assistant`, `homeassistant`, `hass` | [官方参考](https://www.home-assistant.io/installation/) |
| Ruby | Ruby | `ruby` | [官方参考](https://www.ruby-lang.org/en/documentation/) |
| Puma | Ruby | `puma` | [官方参考](https://puma.io/puma/file.systemd.html) |
| Unicorn | Ruby | `unicorn` | [官方参考](https://yhbt.net/unicorn/) |
| Passenger | Ruby | `passenger` | [官方参考](https://www.phusionpassenger.com/docs/) |
| Sidekiq | Ruby | `sidekiq` | [官方参考](https://github.com/sidekiq/sidekiq/wiki/Deployment) |
| Rails | Ruby | `rails`, `rubyonrails` | [官方参考](https://guides.rubyonrails.org/getting_started.html) |
| PHP | PHP | `php` | [官方参考](https://www.php.net/manual/en/) |
| PHP-FPM | PHP | `php-fpm`, `php_fpm` | [官方参考](https://www.php.net/manual/en/install.fpm.php) |
| FrankenPHP | PHP | `frankenphp` | [官方参考](https://frankenphp.dev/docs/) |
| RoadRunner | PHP | `roadrunner` | [官方参考](https://docs.roadrunner.dev/) |
| Swoole | PHP | `swoole`, `openswoole` | [官方参考](https://openswoole.com/docs) |
| Laravel Octane | PHP | `laravel-octane`, `octane` | [官方参考](https://laravel.com/docs/octane) |
| .NET / ASP.NET Core | .NET | `dotnet`, `aspnetcore` | [官方参考](https://learn.microsoft.com/en-us/aspnet/core/host-and-deploy/linux-nginx) |
| MySQL | 数据库 / 缓存 | `mysql`, `mysqld` | [官方参考](https://dev.mysql.com/doc/refman/en/using-systemd.html) |
| MariaDB | 数据库 / 缓存 | `mariadb`, `mariadbd` | [官方参考](https://mariadb.com/kb/en/systemd/) |
| PostgreSQL | 数据库 / 缓存 | `postgres`, `postgresql` | [官方参考](https://www.postgresql.org/docs/current/server-start.html) |
| Redis | 数据库 / 缓存 | `redis`, `redis-server` | [官方参考](https://redis.io/docs/latest/operate/oss_and_stack/) |
| Valkey | 数据库 / 缓存 | `valkey`, `valkey-server` | [官方参考](https://valkey.io/topics/) |
| MongoDB | 数据库 / 缓存 | `mongo`, `mongod`, `mongodb` | [官方参考](https://www.mongodb.com/docs/manual/administration/install-on-linux/) |
| Memcached | 数据库 / 缓存 | `memcached` | [官方参考](https://docs.memcached.org/) |
| ClickHouse | 数据库 / 缓存 | `clickhouse`, `clickhouse-server` | [官方参考](https://clickhouse.com/docs/install) |
| InfluxDB | 数据库 / 缓存 | `influxdb`, `influxd` | [官方参考](https://docs.influxdata.com/) |
| RabbitMQ | 消息 / 搜索 | `rabbitmq`, `rabbitmq-server` | [官方参考](https://www.rabbitmq.com/docs/install-systemd) |
| Kafka | 消息 / 搜索 | `kafka` | [官方参考](https://kafka.apache.org/documentation/) |
| ActiveMQ | 消息 / 搜索 | `activemq`, `artemis` | [官方参考](https://activemq.apache.org/components/artemis/documentation/latest/using-server.html) |
| NATS | 消息 / 搜索 | `nats`, `nats-server` | [官方参考](https://docs.nats.io/running-a-nats-service/introduction/installation) |
| Mosquitto | 消息 / 搜索 | `mosquitto`, `eclipse-mosquitto` | [官方参考](https://mosquitto.org/documentation/) |
| Redpanda | 消息 / 搜索 | `redpanda` | [官方参考](https://docs.redpanda.com/current/deploy/deployment-option/self-hosted/) |
| Elasticsearch | 消息 / 搜索 | `elasticsearch` | [官方参考](https://www.elastic.co/docs/deploy-manage/deploy/self-managed/install-elasticsearch-with-debian-package) |
| OpenSearch | 消息 / 搜索 | `opensearch` | [官方参考](https://docs.opensearch.org/latest/install-and-configure/) |
| etcd | Go 常见应用 | `etcd` | [官方参考](https://etcd.io/docs/) |
| Consul | Go 常见应用 | `consul` | [官方参考](https://developer.hashicorp.com/consul/docs) |
| Vault | Go 常见应用 | `vault` | [官方参考](https://developer.hashicorp.com/vault/docs) |
| Prometheus | 监控 | `prometheus` | [官方参考](https://prometheus.io/docs/prometheus/latest/installation/) |
| Grafana | 监控 | `grafana`, `grafana-server` | [官方参考](https://grafana.com/docs/grafana/latest/setup-grafana/start-restart-grafana/) |
| Loki | 监控 | `loki` | [官方参考](https://grafana.com/docs/loki/latest/setup/install/) |
| Alertmanager | 监控 | `alertmanager`, `prometheus-alertmanager` | [官方参考](https://prometheus.io/docs/alerting/latest/alertmanager/) |
| VictoriaMetrics | 监控 | `victoriametrics`, `victoria-metrics` | [官方参考](https://docs.victoriametrics.com/victoriametrics/quick-start/) |
| FRP 服务端 | 网络 / 穿透 | `frps` | [官方参考](https://gofrp.org/en/docs/) |
| FRP 客户端 | 网络 / 穿透 | `frpc` | [官方参考](https://gofrp.org/en/docs/) |
| Cloudflare Tunnel | 网络 / 穿透 | `cloudflared` | [官方参考](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/) |
| Tailscale | 网络 / 穿透 | `tailscale`, `tailscaled` | [官方参考](https://tailscale.com/kb/) |
| ZeroTier | 网络 / 穿透 | `zerotier`, `zerotier-one` | [官方参考](https://docs.zerotier.com/) |
| MinIO | 常见自托管应用 | `minio` | [官方参考](https://github.com/minio/minio) |
| Syncthing | 常见自托管应用 | `syncthing` | [官方参考](https://docs.syncthing.net/users/autostart.html) |
| Gitea | 常见自托管应用 | `gitea` | [官方参考](https://docs.gitea.com/installation/linux-service) |
| Forgejo | 常见自托管应用 | `forgejo` | [官方参考](https://forgejo.org/docs/latest/admin/) |
| Vaultwarden | 常见自托管应用 | `vaultwarden` | [官方参考](https://github.com/dani-garcia/vaultwarden) |
| Nextcloud | 常见自托管应用 | `nextcloud` | [官方参考](https://docs.nextcloud.com/server/latest/admin_manual/) |
| Immich | 常见自托管应用 | `immich`, `immich-server` | [官方参考](https://docs.immich.app/install/docker-compose/) |
| Jellyfin | 常见自托管应用 | `jellyfin` | [官方参考](https://jellyfin.org/docs/general/installation/) |
| Alist | 常见自托管应用 | `alist` | [官方参考](https://github.com/AlistGo/alist) |
| File Browser | 常见自托管应用 | `filebrowser`, `file-browser` | [官方参考](https://filebrowser.org/) |
| Portainer | 常见自托管应用 | `portainer` | [官方参考](https://docs.portainer.io/) |
| 1Panel | 常见自托管应用 | `1panel`, `1panel-core`, `1panel-agent` | [官方参考](https://1panel.cn/) |
| Sub-Store | 常见自托管应用 | `substore`, `sub-store` | [官方参考](https://github.com/sub-store-org/Sub-Store) |
