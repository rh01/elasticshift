/*
Copyright 2018 The Elasticshift Authors.
*/
package build

import (
	"fmt"
	"os"
	"strconv"

	"github.com/pkg/errors"
	"github.com/elasticshift/elasticshift/api/types"
	"github.com/elasticshift/elasticshift/internal/pkg/graph"
	"github.com/elasticshift/elasticshift/internal/shiftserver/integration"
	itypes "github.com/elasticshift/elasticshift/internal/shiftserver/integration/types"
	"github.com/elasticshift/elasticshift/internal/shiftserver/pubsub"
)

var (
	PATH_WORKER         = "sys/worker"
	defaultStarupScript = `#!/usr/bin/env bash

echo "SHIFT_DIR=${SHIFT_DIR}"
echo "WORKER_URL=${WORKER_URL}"

init() {

  SHIFT_DIR=${1:-/tmp}

  # set the arch
  ARCH=$(uname -m)
  case $ARCH in
    armv5*) ARCH="armv5";;
    armv6*) ARCH="armv6";;
    armv7*) ARCH="armv7";;
    aarch64) ARCH="arm64";;
    x86) ARCH="386";;
    x86_64) ARCH="amd64";;
    i686) ARCH="386";;
    i386) ARCH="386";;
  esac

  # set the os
  OS=$(echo "uname" |tr '[:upper:]' '[:lower:]')

  case "$OS" in
    # Minimalist GNU for Windows
    mingw*) OS='windows';;
  esac
  
  # set timezone to UTC
  ln -sf /usr/share/zoneinfo/Etc/UTC /etc/localtime

  #set sys dir
  SHIFT_SYS_DIR="${SHIFT_DIR}/sys"
}

createUserAndGroup() {

    if [ ! -d "/etc/sudoers.d" ]; then
        mkdir -p /etc/sudoers.d
    fi

    # create shiftuser user/group
    if [ ! $(getent group elasticshift) ]; then
        groupadd --gid 1005 elasticshift \
            && useradd --uid 1005 --gid elasticshift --shell /bin/bash --create-home elasticshift \
            && echo elasticshift ALL=NOPASSWD: ALL >> /etc/sudoers.d/elasticshift
    fi
}

downloadWorker() {
    WORKER_DIST="worker-$OS-$ARCH.tar.gz"
    if [ ! -f "${SHIFT_SYS_DIR}/worker" ]; then
        if type "curl" > /dev/null; then
            curl -SsL --create-dirs "${WORKER_URL}" -o "${SHIFT_SYS_DIR}/${WORKER_DIST}"
        elif type "wget" > /dev/null; then
            wget -P "${SHIFT_SYS_DIR}" -q -O "$WORKER_DIST" "${WORKER_URL}"
        fi

        chown -R elasticshift:elasticshift "${SHIFT_SYS_DIR}"

        tar xf "${SHIFT_SYS_DIR}/${WORKER_DIST}" -C "${SHIFT_SYS_DIR}"
        
        chmod +x "${SHIFT_SYS_DIR}/worker"
    fi
}

startWorker() {
    su elasticshift -c "${SHIFT_SYS_DIR}/worker"
}

#Stop execution on any error
#trap "fail_trap" EXIT
set -e

init
createUserAndGroup
downloadWorker
startWorker`
)

func (r *resolver) GetContainerEngine(team string) (integration.ContainerEngineInterface, error) {

	// Get the default container engine id based on team
	def, err := r.defaultStore.FindByReferenceId(team)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get default container engine:")
	}

	// Get the details of the integration
	var i types.ContainerEngine
	err = r.integrationStore.FindByID(def.ContainerEngineID, &i)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get default integration:")
	}

	// Get the details of the storeage
	var stor types.Storage
	err = r.integrationStore.FindByID(def.StorageID, &stor)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get default storage:")
	}

	// connect to container engine cluster
	return integration.NewContainerEngine(r.loggr, i, stor)
}

func (r *resolver) ContainerLauncher() {

	defer r.recoverErrorIfAny()

	// Restrict the concurrent execution and queue the build
	// if it is for same branch.
	// TODO handle panic
	for b := range r.BuildQueue {

		go func(b types.Build) {

			// start the container
			// TODO select the default orchestration, by config
			// opts := &docker.ClientOptions{}
			// opts.Host = docker.DefaultHost
			// opts.Ctx = r.Ctx

			// cli, err := docker.NewClient(opts)
			// if err != nil {
			// 	r.SLog(b.ID, fmt.Sprintf("Failed to connect to docker daemon: %v", err))
			// }
			buildID := b.ID.Hex()

			var subBuildID int
			subBuildID = 1

			sf, repoFile, err := r.GetShiftfile(b)

			if err != nil {
				//r.SLog(b.ID, fmt.Sprintf("Unable to find the build image from Shiftfile", b.CloneURL))
				sb := types.SubBuild{}
				sb.Image = sf.ImageNames()[0]
				sb.ID = strconv.Itoa(subBuildID)
				sb.Status = types.BuildStatusFailed
				sb.Reason = fmt.Sprintf("Failed to find container image name: %s", err.Error())

				err := r.store.UpdateSubBuild(buildID, sb)
				if err != nil {
					r.logger.Errorf("Error when updating the build status: %v", err)
				}

				r.ps.Publish(pubsub.SubscribeBuildUpdate, buildID)
				return
			}

			// Identify the default orchestration based integration
			// such as docker swarm or kubernetes etc
			engine, err := r.GetContainerEngine(b.Team)
			if err != nil {
				//udpate the build log and set the status to failed
				r.logger.Errorf("Failed to connect container engine: %v", err)

				sb := types.SubBuild{}
				sb.Image = sf.ImageNames()[0]
				sb.ID = strconv.Itoa(subBuildID)
				sb.Status = types.BuildStatusFailed
				sb.Reason = fmt.Sprintf("Failed to launch container: %v", err)

				err := r.store.UpdateSubBuild(buildID, sb)
				if err != nil {
					r.logger.Errorf("Error when updating the build status: %v", err)
				}

				r.ps.Publish(pubsub.SubscribeBuildUpdate, buildID)
				return
			}

			var subBuildExist = true

			for _, imgName := range sf.ImageNames() {

				fmt.Println("Image name: " + imgName)
				subBuildIDStr := strconv.Itoa(subBuildID)

				sb := types.SubBuild{ID: subBuildIDStr, Image: imgName}

				g, err := graph.Construct(sf)
				if err != nil {
					sb.Status = types.BuildStatusFailed
					sb.Reason = fmt.Sprintf("Failed when constructing execution graph: %v", err)

					var err error
					if subBuildExist {
						err = r.store.UpdateSubBuild(buildID, sb)
					} else {
						err = r.store.SaveSubBuild(buildID, &sb)
						subBuildExist = true
					}

					if err != nil {
						r.logger.Errorf("Error when updating the build status: %v", err)
					}

					r.ps.Publish(pubsub.SubscribeBuildUpdate, buildID)
					return
				}

				sb.Graph, _ = g.JSON()
				sb.Image = imgName

				if subBuildExist {
					err = r.store.UpdateSubBuild(buildID, sb)
				} else {
					err = r.store.SaveSubBuild(buildID, &sb)
					subBuildExist = true
				}

				if err != nil {
					r.logger.Errorf("Error when updating the sub build: %v", err)
				}

				// find the system storage
				// storage, err := r.sysconfStore.GetDefaultStorage()
				// if err != nil {
				// 	r.SLog(b.ID, "Failed to fetch the default storage: "+err.Error())
				// 	return
				// }

				// err = utils.Mkdir(filepath.Join(storage.Path, "code", b.Team))
				// if err != nil {
				// 	r.SLog(b.ID, "Unable to create directory for cloning the project:"+err.Error())
				// }

				shiftHost := os.Getenv("SHIFT_HOST")
				if shiftHost == "" {
					shiftHost = "127.0.0.1"
				}

				// env := []string{
				// 	"SHIFT_HOST=shiftserver",
				// 	"SHIFT_PORT=5051",
				// 	"SHIFT_LOGGER=" + LogType_File,
				// 	"SHIFT_BUILDID=" + b.ID.Hex(),
				// 	"SHIFT_TIMEOUT=120m",
				// 	"WORKER_PORT=" + "6060",
				// }

				// filepath.Join(storage.Path, b.Team, DIR_CODE)

				// hc := &container.HostConfig{}
				// hc.Binds = []string{
				// 	filepath.Join(storage.Path, b.Team, DIR_CODE) + ":" + VOL_CODE,
				// 	filepath.Join(storage.Path, b.Team, DIR_LOGS) + ":" + VOL_LOGS,
				// 	filepath.Join(storage.Path, DIR_PLUGINS) + ":" + VOL_PLUGINS,
				// 	filepath.Join(storage.Path, DIR_WORKER) + ":" + VOL_SHIFT,
				// }

				// workerPort, _ := nat.NewPort("tcp", "6060")
				// serverPort, _ := nat.NewPort("tcp", "5051")

				// exposedPorts := map[nat.Port]struct{}{
				// 	serverPort: struct{}{},
				// 	workerPort: struct{}{},
				// }

				// c := &container.Config{
				// 	Image:        imgName,
				// 	Entrypoint:   strslice.StrSlice{"./shift/worker"},
				// 	Env:          env,
				// 	AttachStdout: true,
				// 	ExposedPorts: exposedPorts,
				// }

				envs := []itypes.Env{
					// itypes.Env{"SHIFT_HOST", "shahlab2.duckdns.org"},
					itypes.Env{"SHIFT_HOST", shiftHost},
					itypes.Env{"SHIFT_PORT", "9101"},
					itypes.Env{"SHIFT_BUILDID", b.ID.Hex()},
					itypes.Env{"SHIFT_SUBBUILDID", strconv.Itoa(subBuildID)},
					itypes.Env{"SHIFT_TEAMID", b.Team},
					itypes.Env{"SHIFT_TIMEOUT", "120m"},
					itypes.Env{"WORKER_PORT", "9200"},
					itypes.Env{"SHIFT_LOG_LEVEL", "info"},
					itypes.Env{"SHIFT_LOG_FORMAT", "json"},
					itypes.Env{"SHIFT_REPOFILE", strconv.FormatBool(repoFile)},
				}

				opts := &itypes.CreateContainerOptions{}
				opts.Image = imgName
				// opts.Command = "curl http://shahlab2.duckdns.org:9000/downloads/worker.sh | bash"
				opts.Command = defaultStarupScript
				opts.Environment = envs
				opts.BuildID = b.ID.Hex()
				opts.SubBuildID = subBuildIDStr
				opts.FailureFunc = r.UpdateBuildStatusAsFailed
				opts.UpdateMetadata = r.UpdateBuildMetadata
				// opts.VolumeMounts = []itypes.Volume{{"localvol", "/opt/elasticshift"}}

				res, err := engine.CreateContainer(opts)
				if err != nil {
					r.logger.Errorf("Create container failed: %v", err)
					sb.Status = types.BuildStatusFailed
					sb.Reason = err.Error()

					var err error
					if subBuildExist {
						err = r.store.UpdateSubBuild(buildID, sb)
					} else {
						err = r.store.SaveSubBuild(buildID, &sb)
						subBuildExist = true
					}

					if err != nil {
						r.logger.Errorf("Error when updating the build status: %v", err)
					}

					r.ps.Publish(pubsub.SubscribeBuildUpdate, buildID)
					return
				}

				fmt.Println("Container ID =", res.UID)
				if sb.Metadata == nil {
					sb.Metadata = &types.Metadata{}
				}
				sb.Metadata.ContainerID = res.UID

				if subBuildExist {
					err = r.store.UpdateSubBuild(buildID, sb)
				} else {
					err = r.store.SaveSubBuild(buildID, &sb)
					subBuildExist = true
				}
				if err != nil {
					r.logger.Errorln("Failed to update the container id: ", res.UID)
				}

				subBuildID = subBuildID + 1
				subBuildExist = false
			}

			// err = cli.StartContainer(containerID)
			// if err != nil {
			// 	r.logger.Errorln("Failed to start the container: %v", err)
			// }
		}(b)
	}
}

func (r *resolver) UpdateBuildMetadata(kind int, id, subid, podname string) {

	b, err := r.store.FetchSubBuild(id, subid)
	if err != nil {
		// TODO handle error
	}

	if b.Metadata == nil {
		b.Metadata = &types.Metadata{}
	}
	b.Metadata.Kind = kind
	b.Metadata.PodName = podname

	err = r.store.UpdateSubBuild(id, b)
	if err != nil {
		r.logger.Errorf("Failed to update the build with metadata: %v", err)
	}
}

func (r *resolver) recoverErrorIfAny() {

	if err := recover(); err != nil {
		fmt.Printf("recovered : %v", err)
	}
}
