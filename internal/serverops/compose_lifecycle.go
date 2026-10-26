package serverops

import (
	"errors"
	"slices"
	"strconv"
	"strings"
)

type ComposeMember struct{ ID, Service string }

// ComposeLifecycle controls existing containers without creating or replacing
// them. Its command is a value included in the owner's normal exec approval.
// Both the current project membership and each member's source labels must
// still match the snapshot shown in the confirmation.
func ComposeLifecycle(r ComposeRequest, action string, members []ComposeMember) (string, error) {
	if action != "start" && action != "stop" && action != "restart" {
		return "", errors.New("不支持的项目操作")
	}
	if err := r.Validate(false); err != nil {
		return "", err
	}
	if len(members) == 0 || len(members) > 64 {
		return "", errors.New("项目操作支持 1 至 64 个已有容器")
	}
	rows := slices.Clone(members)
	slices.SortFunc(rows, func(a, b ComposeMember) int { return strings.Compare(a.ID, b.ID) })
	ids, expected, services := []string{}, []string{}, []string{}
	representative := false
	for i, row := range rows {
		if !containerID.MatchString(row.ID) || !serviceName.MatchString(row.Service) || i > 0 && row.ID == rows[i-1].ID {
			return "", errors.New("项目容器身份或服务标签无效")
		}
		representative = representative || row.ID == r.Target.ID
		ids = append(ids, row.ID)
		expected = append(expected, row.ID+" "+row.Service+" ok")
		services = append(services, row.Service)
	}
	slices.Sort(services)
	if !representative || strings.Join(slices.Compact(services), ",") != r.Services {
		return "", errors.New("项目服务范围与确认的容器不一致")
	}
	command, err := r.commandBase(true)
	if err != nil {
		return "", err
	}
	command += "members=$(docker_local container ls --all --no-trunc --filter " + quote("label=com.docker.compose.project="+r.Project) + " --format '{{.ID}}')\n"
	command += "members=$(printf '%s\\n' \"$members\" | LC_ALL=C sort)\n"
	command += "[ \"$members\" = " + quote(strings.Join(ids, "\n")) + " ] || { printf '项目容器已变化，请刷新后重新确认\\n' >&2; exit 78; }\n"
	// A bounded inspect template reads only identities and source labels, never
	// environment variables. Quoted template literals cannot execute paths.
	format := `{{.Id}} {{index .Config.Labels "com.docker.compose.service"}} {{if and (eq (index .Config.Labels "com.docker.compose.project") ` + strconv.Quote(r.Project) + `) (eq (index .Config.Labels "com.docker.compose.project.working_dir") ` + strconv.Quote(r.Directory) + `) (eq (index .Config.Labels "com.docker.compose.project.config_files") ` + strconv.Quote(r.Files) + `)}}ok{{else}}changed{{end}}`
	arguments := ""
	for _, id := range ids {
		arguments += " " + quote(id)
	}
	command += "identities=$(docker_local container inspect --format " + quote(format) + arguments + ")\n"
	command += "[ \"$identities\" = " + quote(strings.Join(expected, "\n")) + " ] || { printf '项目配置来源或服务身份已变化，请重新确认\\n' >&2; exit 78; }\n"
	serviceArgs := ""
	for _, service := range r.ServiceNames() {
		serviceArgs += " " + quote(service)
	}
	switch action {
	case "start":
		command += "compose_local start" + serviceArgs + "\n"
	case "stop":
		command += "compose_local stop --timeout 10" + serviceArgs + "\n"
	case "restart":
		command += "compose_local restart --no-deps --timeout 10" + serviceArgs + "\n"
	}
	if len(command) > 30000 {
		return "", errors.New("项目操作范围过大，无法生成完整确认请求")
	}
	return command, nil
}
