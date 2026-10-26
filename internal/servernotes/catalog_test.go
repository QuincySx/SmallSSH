package servernotes

import (
	"encoding/json"
	"strings"
	"testing"

	"sshclient/internal/monitor"
)

func TestCatalogRecognizesCommonStacksVersionsAndInstances(t *testing.T) {
	for _, tc := range []struct{ unit, want string }{
		{"NGINX.service", "Nginx"}, {"openresty.service", "OpenResty"},
		{"php8.3-fpm.service", "PHP-FPM"}, {"php-fpm82.service", "PHP-FPM"},
		{"tomcat10.service", "Tomcat"}, {"tomcat-11.service", "Tomcat"}, {"tomcat@shop.service", "Tomcat（shop）"},
		{"jetty12.service", "Jetty"}, {"spring-boot.service", "Spring Boot"},
		{"pm2-root.service", "PM2（root）"}, {"pm2-app-user.service", "PM2（app-user）"},
		{"nodejs.service", "Node.js"}, {"bun.service", "Bun"}, {"deno.service", "Deno"},
		{"python3.12.service", "Python"}, {"gunicorn@billing.service", "Gunicorn（billing）"},
		{"uwsgi.service", "uWSGI"}, {"uvicorn@api.service", "Uvicorn（api）"}, {"daphne.service", "Daphne"},
		{"celery@worker.service", "Celery（worker）"}, {"rqworker.service", "RQ"},
		{"puma@shop.service", "Puma（shop）"}, {"ruby3.3.service", "Ruby"}, {"sidekiq.service", "Sidekiq"},
		{"postgresql-16.service", "PostgreSQL"}, {"redis-server.service", "Redis"},
		{"caddy.service", "Caddy"}, {"traefik.service", "Traefik"}, {"minio.service", "MinIO"},
		{"1panel-core.service", "1Panel"}, {"frps.service", "FRP 服务端"}, {"dotnet.service", ".NET / ASP.NET Core"},
	} {
		t.Run(tc.unit, func(t *testing.T) {
			got, ok := identifyService(tc.unit)
			if !ok || got.name != tc.want {
				t.Fatal(tc.unit, got, ok, "want", tc.want)
			}
		})
	}
	for _, unit := range []string{"gssproxy.service", "iprdump.service", "iprinit.service", "iprupdate.service", "systemd-journald.service", "redis-exporter.service", "nginx-exporter.service", "puma-exporter.service", "pm2-exporter.service", "tomcat-helper.service", "node-exporter.service", "governor.service", "go.service", "echo.service", "nginx:evil.service", "nginx\n.service"} {
		if got, ok := identifyService(unit); ok {
			t.Fatal("system plumbing or a partial name became an application", unit, got)
		}
	}
}

func TestCatalogDistinguishesRuntimeContainersAndRepositoryNames(t *testing.T) {
	for _, tc := range []struct{ image, name, want string }{
		{"registry.example.test:5000/team/openresty:1.25@sha256:abc", "web", "OpenResty"},
		{"python:3.12", "orders-api", "Python（orders-api）"},
		{"docker.io/library/node:22-alpine", "web-ui", "Node.js（web-ui）"},
		{"eclipse-temurin:21-jre", "billing", "Java（billing）"},
		{"ruby:3.3", "shop", "Ruby（shop）"},
		{"mcr.microsoft.com/dotnet/aspnet:8.0", "portal", ".NET / ASP.NET Core（portal）"},
		{"example/unknown:v1", "sub-store", "Sub-Store"},
	} {
		got, ok := identifyContainer(monitor.Container{Image: tc.image, Names: tc.name})
		if !ok || got.name != tc.want {
			t.Fatal(tc, got, ok)
		}
	}
	for _, image := range []string{"example/runtime:8.0", "example/nginx-exporter:1", "example/tomcat11-helper:1", "example/private-worker:latest"} {
		if got, ok := identifyContainer(monitor.Container{Image: image, Names: "custom-name"}); ok {
			t.Fatal("image namespace or substring produced an invented application", image, got)
		}
	}
	r := Report{Inventory: monitor.Inventory{Docker: &monitor.DockerInfo{Status: "ready", Containers: []monitor.Container{
		{Names: "orders", Image: "python:3.12", State: "running"},
		{Names: "billing", Image: "python:3.12", State: "running"},
	}}}}
	for _, name := range []string{"Python（orders）", "Python（billing）"} {
		if !strings.Contains(r.Text(), name) {
			t.Fatal("different applications using the same runtime were collapsed", name, r.Text())
		}
	}
}

func TestCatalogRejectsAmbiguousOrInvalidRuleChanges(t *testing.T) {
	base := applicationRule{Name: "Example", Category: "Test", Aliases: []string{"example"}, Source: "https://example.test/docs", Order: 4}
	for _, rules := range [][]applicationRule{
		{}, {base, base},
		{func() applicationRule { v := base; v.UnitPatterns = []string{"example"}; return v }()},
		{func() applicationRule { v := base; v.UnitPatterns = []string{"^["}; return v }()},
		{func() applicationRule { v := base; v.Excludes = []string{"example"}; return v }()},
		{func() applicationRule { v := base; v.Source = "http://example.test/docs"; return v }()},
	} {
		data, _ := json.Marshal(rules)
		if _, err := parseCatalog(data); err == nil {
			t.Fatal("invalid catalog change accepted", string(data))
		}
	}
	if _, err := parseCatalog(append(append([]byte(nil), catalogData...), []byte(" true")...)); err == nil {
		t.Fatal("trailing configuration data accepted")
	}
}

func BenchmarkCommonServiceRecognition(b *testing.B) {
	units := []string{"nginx.service", "gunicorn@api.service", "pm2-root.service", "tomcat10.service", "gssproxy.service", "custom-api.service"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		identifyService(units[i%len(units)])
	}
}
