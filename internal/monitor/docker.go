package monitor

import (
	"encoding/json"
	"strings"
)

// Explicit local sockets prevent a saved Docker context/DOCKER_HOST on the
// server from silently selecting another machine. Never reads container envs.
const dockerPrelude = `(
printf '\n@@DOCKER_STATUS\n'
if ! command -v docker >/dev/null 2>&1; then printf 'missing\n'; exit 0; fi
printf 'checking\n'
printf '\n@@DOCKER_CLIENT\n'; docker --version 2>/dev/null
unset DOCKER_HOST DOCKER_CONTEXT DOCKER_TLS_VERIFY DOCKER_CERT_PATH
socket=/var/run/docker.sock
uid=$(id -u)
if [ "$uid" != 0 ] && [ -S "/run/user/$uid/docker.sock" ]; then socket="/run/user/$uid/docker.sock"; fi
printf '\n@@DOCKER_SOCKET\n%s\n' "$socket"
docker_local() { docker --host "unix://$socket" "$@"; }
if info=$(docker_local info --format '{"ID":{{json .ID}},"Version":{{json .ServerVersion}}}' 2>&1); then
 printf '\n@@DOCKER_INFO\n%s\n' "$info"
`

const DockerCommand = dockerPrelude + ` printf '\n@@DOCKER_CONTAINERS\n'
 if rows=$(docker_local container ls --all --no-trunc --format '{"ID":{{json .ID}},"Names":{{json .Names}},"Image":{{json .Image}},"State":{{json .State}},"Status":{{json .Status}},"Ports":{{json .Ports}},"Project":{{json (.Label "com.docker.compose.project")}},"Service":{{json (.Label "com.docker.compose.service")}},"ComposeDir":{{json (.Label "com.docker.compose.project.working_dir")}},"ComposeFiles":{{json (.Label "com.docker.compose.project.config_files")}}}' 2>&1); then printf '%s\n' "$rows" | head -n 1025
 else printf '\n@@DOCKER_ERROR\n%s\n' "$rows"; fi
 printf '\n@@DOCKER_IMAGES\n'
 if rows=$(docker_local image ls --no-trunc --format '{{json .}}' 2>&1); then printf '%s\n' "$rows" | head -n 1025
 else printf '\n@@DOCKER_ERROR\n%s\n' "$rows"; fi
` + dockerEpilogue

// Slow Docker sampling is independent from the two-second /proc loop.
const DockerStatsCommand = dockerPrelude + `
 printf '\n@@DOCKER_STATS\n'
 if ids=$(docker_local container ls --quiet --no-trunc 2>&1); then
  ids=$(printf '%s\n' "$ids" | awk 'length($0)==64 && $0 ~ /^[a-f0-9]+$/ {if (++n<=129) print}')
  if [ -n "$ids" ]; then
   set -- $ids
   limited=0
   if [ "$#" -gt 128 ]; then limited=1; ids=$(printf '%s\n' "$ids" | head -n 128); set -- $ids; fi
   if rows=$(docker_local container stats --no-stream --no-trunc --format '{{json .}}' "$@" 2>&1); then printf '%s\n' "$rows"
   else printf '\n@@DOCKER_ERROR\n%s\n' "$rows"; fi
   if [ "$limited" = 1 ]; then printf '\n@@DOCKER_ERROR\n只采样前 128 个运行中的容器\n'; fi
  fi
 else printf '\n@@DOCKER_ERROR\n%s\n' "$ids"; fi
` + dockerEpilogue

const dockerEpilogue = `
 printf '\n@@DOCKER_STATUS\nready\n'
else
 printf '\n@@DOCKER_STATUS\nunavailable\n\n@@DOCKER_ERROR\n%s\n' "$info"
fi
)
`

type Container struct {
	ID, Names, Image, State, Status, Ports, Labels, Project string
	Service, ComposeDir, ComposeFiles                       string
}

type Image struct {
	ID, Repository, Tag, Size, CreatedSince, CreatedAt string
}

type DockerInfo struct {
	Status, ClientVersion, Version, Socket, EngineID, Note string
	Containers                                             []Container
	Images                                                 []Image
	Resources                                              []ContainerStats
}

func (d *DockerInfo) parse(section, line string) {
	switch section {
	case "@@DOCKER_STATUS":
		d.Status = line
	case "@@DOCKER_CLIENT":
		d.ClientVersion = line
	case "@@DOCKER_SOCKET":
		d.Socket = line
	case "@@DOCKER_INFO":
		var info struct{ ID, Version string }
		if json.Unmarshal([]byte(line), &info) == nil {
			d.EngineID, d.Version = info.ID, info.Version
		}
	case "@@DOCKER_CONTAINERS":
		var row Container
		if json.Unmarshal([]byte(line), &row) != nil || row.ID == "" {
			d.Note = "容器清单读取不完整"
			return
		}
		if len(d.Containers) >= 1024 {
			d.Note = "只显示前 1024 项；清单已截断"
			return
		}
		d.Containers = append(d.Containers, row)
	case "@@DOCKER_IMAGES":
		var row Image
		if json.Unmarshal([]byte(line), &row) != nil || row.ID == "" {
			d.Note = "镜像清单读取不完整"
			return
		}
		if len(d.Images) >= 1024 {
			d.Note = "只显示前 1024 项；清单已截断"
			return
		}
		d.Images = append(d.Images, row)
	case "@@DOCKER_STATS":
		if len(d.Resources) >= 128 {
			d.Note = "只采样前 128 个运行中的容器"
			return
		}
		if row, ok := parseContainerStats(line); ok {
			d.Resources = append(d.Resources, row)
		} else {
			d.Note = "容器资源读取不完整"
		}

	case "@@DOCKER_ERROR":
		d.Note = strings.TrimSpace(d.Note + " " + line)
		if len(d.Note) > 1024 {
			d.Note = d.Note[:1024]
		}
	}
}
