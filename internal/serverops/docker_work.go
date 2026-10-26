package serverops

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

// All request fields are values: approval copies cannot alias mutable targets.
type DockerTarget struct{ Socket, Engine, ID string }
type DockerRequest struct {
	Target DockerTarget
	Shell  string
	Tail   int
	Follow bool
}
type ComposeRequest struct {
	Target                                          DockerTarget
	Project, Directory, Files, Services, ConfigHash string
}
type ComposePlan struct {
	Hash             string `json:"hash"`
	Services, Images []string
	Version          string
}
type ComposeStatus struct {
	ID, Name, Service, State, Health, Image string
	ExitCode                                int
}

var composeName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)
var serviceName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (t DockerTarget) Validate() error {
	if len(t.Socket) > 128 || t.Socket != "/var/run/docker.sock" && !rootlessSocket.MatchString(t.Socket) {
		return errors.New("不支持的 Docker 本地接口")
	}
	if !engineID.MatchString(t.Engine) || !containerID.MatchString(t.ID) {
		return errors.New("容器或 Docker 身份不完整，请刷新后重试")
	}
	return nil
}

func (r DockerRequest) Validate(kind string) error {
	if err := r.Target.Validate(); err != nil {
		return err
	}
	switch kind {
	case "docker-logs":
		if r.Tail < 1 || r.Tail > 2000 || r.Shell != "" {
			return errors.New("日志范围需为 1 到 2000 行")
		}
	case "docker-terminal":
		if r.Tail != 0 || r.Follow || r.Shell != "sh" && r.Shell != "bash" {
			return errors.New("请选择 sh 或 bash 容器终端")
		}
	default:
		return errors.New("不支持的 Docker 操作")
	}
	return nil
}

func dockerGuard(t DockerTarget) string {
	return fmt.Sprintf(`set -eu
unset DOCKER_HOST DOCKER_CONTEXT DOCKER_TLS_VERIFY DOCKER_CERT_PATH
docker_local() { docker --host %s "$@"; }
actual=$(docker_local info --format '{{.ID}}')
[ "$actual" = %s ] || { printf 'Docker 实例已变化，请刷新后重试\n' >&2; exit 78; }
`, quote("unix://"+t.Socket), quote(t.Engine))
}

func (r DockerRequest) Command(kind string) (string, error) {
	if err := r.Validate(kind); err != nil {
		return "", err
	}
	command := dockerGuard(r.Target)
	if kind == "docker-logs" {
		follow := ""
		if r.Follow {
			follow = " --follow"
		}
		command += fmt.Sprintf("docker_local container logs --timestamps --tail %d%s %s\n", r.Tail, follow, quote(r.Target.ID))
	} else {
		command += fmt.Sprintf("exec docker --host %s container exec --interactive --tty %s %s\n", quote("unix://"+r.Target.Socket), quote(r.Target.ID), quote(r.Shell))
	}
	return command, nil
}

func (r ComposeRequest) ServiceNames() []string { return strings.Split(r.Services, ",") }
func (r ComposeRequest) Validate(update bool) error {
	if err := r.Target.Validate(); err != nil {
		return err
	}
	if !composeName.MatchString(r.Project) || !safeComposePath(r.Directory) || len(r.Files) > 16384 || len(r.Services) > 4096 {
		return errors.New("Compose 项目来源不完整")
	}
	files := strings.Split(r.Files, ",")
	if len(files) > 16 {
		return errors.New("Compose 配置文件过多")
	}
	for _, file := range files {
		if !safeComposePath(file) {
			return errors.New("无法识别 Compose 配置文件路径，请在服务器核对")
		}
	}
	services := r.ServiceNames()
	if len(services) > 32 || !slices.IsSorted(services) {
		return errors.New("请选择至多 32 个服务")
	}
	for n, name := range services {
		if !serviceName.MatchString(name) || n > 0 && name == services[n-1] {
			return errors.New("Compose 服务名称无效或重复")
		}
	}
	if update && !digest.MatchString(r.ConfigHash) || !update && r.ConfigHash != "" {
		return errors.New("Compose 更新需先核对配置计划")
	}
	return nil
}
func safeComposePath(s string) bool {
	return len(s) > 0 && len(s) <= 4096 && strings.HasPrefix(s, "/") && path.Clean(s) == s && !strings.ContainsAny(s, "\x00\r\n")
}

// Preserve file order (override semantics). Never inherit a caller's Compose
// project/context flags, run a build, remove volumes or remove orphan services.
func (r ComposeRequest) commandBase(checkSource bool) (string, error) {
	check := r
	check.ConfigHash = ""
	if err := check.Validate(false); err != nil {
		return "", err
	}
	command := dockerGuard(r.Target)
	command += "unset COMPOSE_FILE COMPOSE_PROJECT_NAME COMPOSE_PROFILES COMPOSE_ENV_FILES COMPOSE_PATH_SEPARATOR COMPOSE_DISABLE_ENV_FILE\n"
	command += "unset COMPOSE_DRY_RUN COMPOSE_EXPERIMENTAL_DRY_RUN DOCKER_DEFAULT_PLATFORM\nCOMPOSE_REMOVE_ORPHANS=0 COMPOSE_MENU=0 COMPOSE_STATUS_STDOUT=0\nexport COMPOSE_REMOVE_ORPHANS COMPOSE_MENU COMPOSE_STATUS_STDOUT\n"
	command += "cd -- " + quote(r.Directory) + "\n"
	args := "--ansi never --project-name " + quote(r.Project) + " --project-directory " + quote(r.Directory) + " --profile '*'"
	for _, file := range strings.Split(r.Files, ",") {
		args += " --file " + quote(file)
	}
	command += "compose_local() { docker_local compose " + args + " \"$@\"; }\n"
	if checkSource {
		for _, v := range []struct{ label, expected string }{{"com.docker.compose.project", r.Project}, {"com.docker.compose.project.working_dir", r.Directory}, {"com.docker.compose.project.config_files", r.Files}} {
			command += fmt.Sprintf("value=$(docker_local container inspect --format %s %s)\n[ \"$value\" = %s ] || { printf 'Compose 来源已变化，请刷新后重新确认\\n' >&2; exit 78; }\n", quote(`{{index .Config.Labels "`+v.label+`"}}`), quote(r.Target.ID), quote(v.expected))
		}
	}
	return command, nil
}

func (r ComposeRequest) Command(stage string) (string, error) {
	command, err := r.commandBase(stage != "status")
	if err != nil {
		return "", err
	}
	services := ""
	for _, name := range r.ServiceNames() {
		services += " " + quote(name)
	}
	switch stage {
	case "plan":
		command += "printf '\\n@@COMPOSE_VERSION\\n'\ndocker_local compose version --short\n"
		// Require --wait before any mutation; old Compose versions fail early.
		command += "compose_local up --help | grep -q -- '--wait-timeout' || { printf 'Compose 版本过旧，需要支持 --wait-timeout 的版本\\n' >&2; exit 69; }\n"
		// The broker hashes the resolved model privately and returns only
		// service/image names. Config/env contents never enter log output.
		command += "printf '\\n@@COMPOSE_MODEL\\n'\ncompose_local config --format json" + services + "\n"
	case "pull":
		command += "compose_local pull --policy always" + services + "\n"
	case "up":
		command += "compose_local up --detach --no-deps --no-build --pull never --wait --wait-timeout 120 --timeout 10 --remove-orphans=false" + services + "\n"
	case "status":
		command += "compose_local ps --all --format json" + services + "\n"
	default:
		return "", errors.New("不支持的 Compose 阶段")
	}
	return command, nil
}

func ParseComposePlan(r ComposeRequest, output string) (ComposePlan, error) {
	var p ComposePlan
	if len(output) > 1<<20 {
		return p, errors.New("Compose 计划过大")
	}
	version, model, ok := strings.Cut(output, "\n@@COMPOSE_MODEL\n")
	p.Version = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(version), "@@COMPOSE_VERSION"))
	if !ok || p.Version == "" || len(p.Version) > 64 || strings.ContainsAny(p.Version, "\r\n\x1b") {
		return p, errors.New("Compose 计划不完整")
	}
	var config struct {
		Name     string
		Services map[string]struct{ Image string }
	}
	if err := json.Unmarshal([]byte(model), &config); err != nil || config.Name != r.Project {
		return p, errors.New("Compose 配置无法核对或项目名称已变化")
	}
	p.Services = r.ServiceNames()
	for _, name := range p.Services {
		service, exists := config.Services[name]
		if !exists {
			return p, fmt.Errorf("Compose 服务 %s 已变化，请重新选择", name)
		}
		if service.Image == "" {
			return p, fmt.Errorf("服务 %s 只有本地构建配置，请先发布镜像并设置 image", name)
		}
		if len(service.Image) > 512 || strings.ContainsAny(service.Image, " \t\r\n\x1b") {
			return p, errors.New("Compose 镜像名称无效")
		}
		p.Images = append(p.Images, service.Image)
	}
	slices.Sort(p.Images)
	p.Images = slices.Compact(p.Images)
	r.ConfigHash = ""
	var canonical any
	decoder := json.NewDecoder(strings.NewReader(model))
	decoder.UseNumber()
	if err := decoder.Decode(&canonical); err != nil {
		return p, errors.New("Compose 配置无法解析")
	}
	b, _ := json.Marshal(struct {
		Request ComposeRequest
		Model   any
		Version string
	}{r, canonical, p.Version})
	sum := sha256.Sum256(b)
	p.Hash = hex.EncodeToString(sum[:])
	return p, nil
}

func ParseComposeStatus(output string, services []string) ([]ComposeStatus, error) {
	if len(output) > 1<<20 {
		return nil, errors.New("Compose 状态过大")
	}
	var rows []ComposeStatus
	trim := strings.TrimSpace(output)
	if strings.HasPrefix(trim, "[") {
		if err := json.Unmarshal([]byte(trim), &rows); err != nil {
			return nil, err
		}
	} else {
		scanner := bufio.NewScanner(strings.NewReader(trim))
		scanner.Buffer(make([]byte, 4096), 64<<10)
		for scanner.Scan() {
			var row ComposeStatus
			if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
				return nil, err
			}
			rows = append(rows, row)
			if len(rows) > 1024 {
				return nil, errors.New("Compose 状态项过多")
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	seen := map[string]bool{}
	if len(rows) > 1024 {
		return nil, errors.New("Compose 状态项过多")
	}
	for _, row := range rows {
		if !slices.Contains(services, row.Service) {
			return rows, errors.New("Compose 返回了未选择的服务")
		}
		seen[row.Service] = true
		if row.State != "running" && !(row.State == "exited" && row.ExitCode == 0) || row.Health != "" && row.Health != "healthy" {
			return rows, fmt.Errorf("服务 %s 尚未就绪：%s %s", row.Service, row.State, row.Health)
		}
	}
	for _, name := range services {
		if !seen[name] {
			return rows, fmt.Errorf("未读取到服务 %s 的更新结果", name)
		}
	}
	return rows, nil
}
