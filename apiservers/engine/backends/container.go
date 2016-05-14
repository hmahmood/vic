// Copyright 2016 VMware, Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package vicbackends

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	goexec "os/exec"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/docker/api/types/backend"
	derr "github.com/docker/docker/errors"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/version"
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/container"
	"github.com/docker/engine-api/types/strslice"

	viccontainer "github.com/vmware/vic/apiservers/engine/backends/container"
	"github.com/vmware/vic/apiservers/portlayer/client/exec"
	"github.com/vmware/vic/apiservers/portlayer/client/interaction"
	"github.com/vmware/vic/apiservers/portlayer/client/storage"
	"github.com/vmware/vic/apiservers/portlayer/models"
	"github.com/vmware/vic/pkg/attachutils"
	"github.com/vmware/vic/pkg/trace"
	//	"github.com/vmware/vic/pkg/docker/daemon"
)

type Container struct {
	ProductName string
}

// docker's container.execBackend

// ContainerExecCreate sets up an exec in a running container.
func (c *Container) ContainerExecCreate(config *types.ExecConfig) (string, error) {
	return "", fmt.Errorf("%s does not implement container.ContainerExecCreate", c.ProductName)
}

// ContainerExecInspect returns low-level information about the exec
// command. An error is returned if the exec cannot be found.
func (c *Container) ContainerExecInspect(id string) (*backend.ExecInspect, error) {
	return nil, fmt.Errorf("%s does not implement container.ContainerExecInspect", c.ProductName)
}

// ContainerExecResize changes the size of the TTY of the process
// running in the exec with the given name to the given height and
// width.
func (c *Container) ContainerExecResize(name string, height, width int) error {
	return nil
	//	return fmt.Errorf("%s does not implement container.ContainerExecResize", c.ProductName)
}

// ContainerExecStart starts a previously set up exec instance. The
// std streams are set up.
func (c *Container) ContainerExecStart(name string, stdin io.ReadCloser, stdout io.Writer, stderr io.Writer) error {
	return fmt.Errorf("%s does not implement container.ContainerExecStart", c.ProductName)
}

// ExecExists looks up the exec instance and returns a bool if it exists or not.
// It will also return the error produced by `getConfig`
func (c *Container) ExecExists(name string) (bool, error) {
	return false, fmt.Errorf("%s does not implement container.ExecExists", c.ProductName)
}

// docker's container.copyBackend

// ContainerArchivePath creates an archive of the filesystem resource at the
// specified path in the container identified by the given name. Returns a
// tar archive of the resource and whether it was a directory or a single file.
func (c *Container) ContainerArchivePath(name string, path string) (content io.ReadCloser, stat *types.ContainerPathStat, err error) {
	return nil, nil, fmt.Errorf("%s does not implement container.ContainerArchivePath", c.ProductName)
}

// ContainerCopy performs a deprecated operation of archiving the resource at
// the specified path in the container identified by the given name.
func (c *Container) ContainerCopy(name string, res string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("%s does not implement container.ContainerCopy", c.ProductName)
}

// ContainerExport writes the contents of the container to the given
// writer. An error is returned if the container cannot be found.
func (c *Container) ContainerExport(name string, out io.Writer) error {
	return fmt.Errorf("%s does not implement container.ContainerExport", c.ProductName)
}

// ContainerExtractToDir extracts the given archive to the specified location
// in the filesystem of the container identified by the given name. The given
// path must be of a directory in the container. If it is not, the error will
// be ErrExtractPointNotDirectory. If noOverwriteDirNonDir is true then it will
// be an error if unpacking the given content would cause an existing directory
// to be replaced with a non-directory and vice versa.
func (c *Container) ContainerExtractToDir(name, path string, noOverwriteDirNonDir bool, content io.Reader) error {
	return fmt.Errorf("%s does not implement container.ContainerExtractToDir", c.ProductName)
}

// ContainerStatPath stats the filesystem resource at the specified path in the
// container identified by the given name.
func (c *Container) ContainerStatPath(name string, path string) (stat *types.ContainerPathStat, err error) {
	return nil, fmt.Errorf("%s does not implement container.ContainerStatPath", c.ProductName)
}

// docker's container.stateBackend

// ContainerCreate creates a container.
func (c *Container) ContainerCreate(config types.ContainerCreateConfig) (types.ContainerCreateResponse, error) {
	defer trace.End(trace.Begin("ContainerCreate"))

	var err error

	//TODO: validate the config parameters
	log.Printf("config.Config = %+v", config.Config)

	// Get an API client to the portlayer
	client := PortLayerClient()
	if client == nil {
		return types.ContainerCreateResponse{},
			derr.NewErrorWithStatusCode(fmt.Errorf("container.ContainerCreate failed to create a portlayer client"),
				http.StatusInternalServerError)
	}

	var layer *viccontainer.VicContainer
	container := viccontainer.GetCache().GetContainerByName(config.Config.Image)
	if container == nil {
		layer, err = c.getImageMetadataFromStoragePL(config.Config.Image)

		if err != nil {
			return types.ContainerCreateResponse{}, err
		}
	}

	// Overwrite or append the image's config from the CLI with the metadata from the image's
	// layer metadata where appropriate
	if len(config.Config.Cmd) == 0 {
		config.Config.Cmd = layer.Config.Cmd
	}
	if config.Config.WorkingDir == "" {
		config.Config.WorkingDir = layer.Config.WorkingDir
	}
	if len(config.Config.Entrypoint) == 0 {
		config.Config.Entrypoint = layer.Config.Entrypoint
	}
	config.Config.Env = append(config.Config.Env, layer.Config.Env...)

	log.Printf("config.Config' = %+v", config.Config)

	// Call the Exec port layer to create the container
	host, err := os.Hostname()
	if err != nil {
		return types.ContainerCreateResponse{},
			derr.NewErrorWithStatusCode(fmt.Errorf("container.ContainerCreate got unexpected error getting hostname"),
				http.StatusInternalServerError)
	}

	plCreateParams := c.dockerContainerCreateParamsToPortlayer(config, layer.ID, host)
	createResults, err := client.Exec.ContainerCreate(plCreateParams)

	// transfer port layer swagger based response to Docker backend data structs and return to the REST front-end
	if err != nil {
		if _, isa := err.(*exec.ContainerCreateNotFound); isa {
			return types.ContainerCreateResponse{}, derr.NewRequestNotFoundError(fmt.Errorf("No such image: %s", layer.ID))
		}

		// If we get here, most likely something went wrong with the port layer API server
		return types.ContainerCreateResponse{},
			derr.NewErrorWithStatusCode(fmt.Errorf("Unknown error from the exec port layer"), http.StatusInternalServerError)
	}

	// Container created ok, overwrite the container params in the container store as
	// these are the parameters that the containers were actually created with
	layer.Config.Cmd = config.Config.Cmd
	layer.Config.WorkingDir = config.Config.WorkingDir
	layer.Config.Entrypoint = config.Config.Entrypoint
	layer.Config.Env = config.Config.Env
	layer.ContainerID = *createResults.Payload.ContainerID

	viccontainer.GetCache().SaveContainer(*createResults.Payload.ContainerID, layer)

	// Success!
	log.Printf("container.ContainerCreate succeeded.  Returning container id %s", *createResults.Payload.ContainerID)
	return types.ContainerCreateResponse{ID: *createResults.Payload.ContainerID}, nil
}

// ContainerKill sends signal to the container
// If no signal is given (sig 0), then Kill with SIGKILL and wait
// for the container to exit.
// If a signal is given, then just send it to the container and return.
func (c *Container) ContainerKill(name string, sig uint64) error {
	return fmt.Errorf("%s does not implement container.ContainerKill", c.ProductName)
}

// ContainerPause pauses a container
func (c *Container) ContainerPause(name string) error {
	return fmt.Errorf("%s does not implement container.ContainerPause", c.ProductName)
}

// ContainerRename changes the name of a container, using the oldName
// to find the container. An error is returned if newName is already
// reserved.
func (c *Container) ContainerRename(oldName, newName string) error {
	return fmt.Errorf("%s does not implement container.ContainerRename", c.ProductName)
}

// ContainerResize changes the size of the TTY of the process running
// in the container with the given name to the given height and width.
func (c *Container) ContainerResize(name string, height, width int) error {
	//	defer trace.End(trace.Begin("ContainerResize"))

	// Get an API client to the portlayer
	client := PortLayerClient()
	if client == nil {
		return derr.NewErrorWithStatusCode(fmt.Errorf("container.ContainerResize failed to create a portlayer client"),
			http.StatusInternalServerError)
	}

	// Call the port layer to resize
	plHeight := int32(height)
	plWidth := int32(width)
	plResizeParam := interaction.NewContainerResizeParams().WithID(name).WithHeight(plHeight).WithWidth(plWidth)

	_, err := client.Interaction.ContainerResize(plResizeParam)
	if err != nil {
		if _, isa := err.(*interaction.ContainerResizeNotFound); isa {
			return derr.NewRequestNotFoundError(fmt.Errorf("No such container: %s", name))
		}

		// If we get here, most likely something went wrong with the port layer API server
		return derr.NewErrorWithStatusCode(fmt.Errorf("Unknown error from the exec port layer"),
			http.StatusInternalServerError)
	}

	return nil
}

// ContainerRestart stops and starts a container. It attempts to
// gracefully stop the container within the given timeout, forcefully
// stopping it if the timeout is exceeded. If given a negative
// timeout, ContainerRestart will wait forever until a graceful
// stop. Returns an error if the container cannot be found, or if
// there is an underlying error at any stage of the restart.
func (c *Container) ContainerRestart(name string, seconds int) error {
	return fmt.Errorf("%s does not implement container.ContainerRestart", c.ProductName)
}

// ContainerRm removes the container id from the filesystem. An error
// is returned if the container is not found, or if the remove
// fails. If the remove succeeds, the container name is released, and
// network links are removed.
func (c *Container) ContainerRm(name string, config *types.ContainerRmConfig) error {
	return fmt.Errorf("%s does not implement container.ContainerRm", c.ProductName)
}

// ContainerStart starts a container.
func (c *Container) ContainerStart(name string, hostConfig *container.HostConfig) error {
	defer trace.End(trace.Begin("ContainerStart"))

	// Get an API client to the portlayer
	client := PortLayerClient()
	if client == nil {
		return derr.NewErrorWithStatusCode(fmt.Errorf("container.ContainerCreate failed to create a portlayer client"),
			http.StatusInternalServerError)
	}

	// handle legancy hostConfig
	if hostConfig != nil {
		// hostConfig exist for backwards compatibility.  TODO: Figure out which parameters we
		// need to look at in hostConfig
	}

	// Start the container
	// TODO: We need a resolved ID from the name
	plStartParams := exec.NewContainerStartParams().WithID(name)
	_, err := client.Exec.ContainerStart(plStartParams)
	if err != nil {
		if _, isa := err.(*exec.ContainerStartNotFound); isa {
			return derr.NewRequestNotFoundError(fmt.Errorf("No such container: %s", name))
		}

		// If we get here, most likely something went wrong with the port layer API server
		return derr.NewErrorWithStatusCode(fmt.Errorf("Unknown error from the exec port layer"),
			http.StatusInternalServerError)
	}

	return nil
}

// ContainerStop looks for the given container and terminates it,
// waiting the given number of seconds before forcefully killing the
// container. If a negative number of seconds is given, ContainerStop
// will wait for a graceful termination. An error is returned if the
// container is not found, is already stopped, or if there is a
// problem stopping the container.
func (c *Container) ContainerStop(name string, seconds int) error {
	return fmt.Errorf("%s does not implement container.ContainerStop", c.ProductName)
}

// ContainerUnpause unpauses a container
func (c *Container) ContainerUnpause(name string) error {
	return fmt.Errorf("%s does not implement container.ContainerUnpause", c.ProductName)
}

// ContainerUpdate updates configuration of the container
func (c *Container) ContainerUpdate(name string, hostConfig *container.HostConfig) ([]string, error) {
	return make([]string, 0, 0), fmt.Errorf("%s does not implement container.ContainerUpdate", c.ProductName)
}

// ContainerWait stops processing until the given container is
// stopped. If the container is not found, an error is returned. On a
// successful stop, the exit code of the container is returned. On a
// timeout, an error is returned. If you want to wait forever, supply
// a negative duration for the timeout.
func (c *Container) ContainerWait(name string, timeout time.Duration) (int, error) {
	return 0, fmt.Errorf("%s does not implement container.ContainerWait", c.ProductName)
}

// docker's container.monitorBackend

// ContainerChanges returns a list of container fs changes
func (c *Container) ContainerChanges(name string) ([]archive.Change, error) {
	return make([]archive.Change, 0, 0), fmt.Errorf("%s does not implement container.ContainerChanges", c.ProductName)
}

// ContainerInspect returns low-level information about a
// container. Returns an error if the container cannot be found, or if
// there is an error getting the data.
func (c *Container) ContainerInspect(name string, size bool, version version.Version) (interface{}, error) {
	//Ignore version.  We're supporting post-1.20 version.

	defer trace.End(trace.Begin("ContainerInspect"))

	// Look up the container info in the metadata cache
	vc := viccontainer.GetCache().GetContainerByName(name)
	if vc == nil {
		return nil, derr.NewRequestNotFoundError(fmt.Errorf("No such container: %s", name))
	}

	// HACK: Just set a bunch of dummy info needed to get docker attach working
	base := &types.ContainerJSONBase{
		State: &types.ContainerState{Status: "running",
			Running: true,
			Paused:  false,
		},
	}

	conJSON := &types.ContainerJSON{ContainerJSONBase: base, Config: vc.Config}

	log.Printf("ContainerInspect json config = %+v\n", conJSON.Config)

	return conJSON, nil
}

// ContainerLogs hooks up a container's stdout and stderr streams
// configured with the given struct.
func (c *Container) ContainerLogs(name string, config *backend.ContainerLogsConfig, started chan struct{}) error {
	return fmt.Errorf("%s does not implement container.ContainerLogs", c.ProductName)
}

// ContainerStats writes information about the container to the stream
// given in the config object.
func (c *Container) ContainerStats(name string, config *backend.ContainerStatsConfig) error {
	return fmt.Errorf("%s does not implement container.ContainerStats", c.ProductName)
}

// ContainerTop lists the processes running inside of the given
// container by calling ps with the given args, or with the flags
// "-ef" if no args are given.  An error is returned if the container
// is not found, or is not running, or if there are any problems
// running ps, or parsing the output.
func (c *Container) ContainerTop(name string, psArgs string) (*types.ContainerProcessList, error) {
	return nil, fmt.Errorf("%s does not implement container.ContainerTop", c.ProductName)
}

// Containers returns the list of containers to show given the user's filtering.
func (c *Container) Containers(config *types.ContainerListOptions) ([]*types.Container, error) {
	return nil, fmt.Errorf("%s does not implement container.Containers", c.ProductName)
}

// docker's container.attachBackend

// ContainerAttach attaches to logs according to the config passed in. See ContainerAttachConfig.
func (c *Container) ContainerAttach(prefixOrName string, ca *backend.ContainerAttachConfig) error {
	vc := viccontainer.GetCache().GetContainerByName(prefixOrName)

	//FIXME: Call the exec portlayer and get the current status of the container.
	// If the container is not running, return an error

	if vc == nil {
		vc = viccontainer.NewVicContainer()
	}

	conn, br, err := attachutils.AttachToDockerLikeServer(
		PortLayerServer(),
		vc.ContainerID,
		"attach",
		ca.UseStdin, ca.UseStdout, ca.UseStderr, ca.Stream)

	if err != nil {
		log.Errorf("ContainerAttached received error attaching to portlayer: %s", err)
		return err
	}

	inStream, outStream, errStream, err := ca.GetStreams()

	// Starts up the stream copying and wait for them to stop or error.  The
	// streams are from the hijacked connection of this API server.  The conn
	// is the connection to the portlayer server.
	err = attachutils.CopyWaitConnToStreams(
		true,
		inStream,
		outStream,
		errStream,
		conn,
		br)

	if err != nil {
		log.Errorf("ContainerAttach received error waiting on streams: %s", err)
	}

	return err
}

//----------
// Utility Functions
//----------

func (c *Container) dockerContainerCreateParamsToPortlayer(cc types.ContainerCreateConfig, layerID string, imageStore string) *exec.ContainerCreateParams {
	config := &models.ContainerCreateConfig{}

	// Image
	config.Image = new(string)
	*config.Image = layerID

	var path string
	var args []string

	// Expand cmd into entrypoint and args
	cmd := strslice.StrSlice(cc.Config.Cmd)
	if len(cc.Config.Entrypoint) != 0 {
		path, args = cc.Config.Entrypoint[0], append(cc.Config.Entrypoint[1:], cmd...)
	} else {
		path, args = cmd[0], cmd[1:]
	}

	// copy the path
	config.Path = new(string)
	*config.Path = path

	// copy the args
	config.Args = make([]string, len(args))
	copy(config.Args, args)

	// copy the env array
	config.Env = make([]string, len(cc.Config.Env))
	copy(config.Env, cc.Config.Env)

	// image store
	config.ImageStore = &models.ImageStore{Name: imageStore}

	// network
	config.NetworkDisabled = new(bool)
	*config.NetworkDisabled = cc.Config.NetworkDisabled
	config.NetworkSettings = toModelsNetworkConfig(cc)

	// working dir
	config.WorkingDir = new(string)
	*config.WorkingDir = cc.Config.WorkingDir

	log.Printf("dockerContainerCreateParamsToPortlayer = %+v", config)
	//TODO: Fill in the name
	return exec.NewContainerCreateParams().WithCreateConfig(config)
}

func toModelsNetworkConfig(cc types.ContainerCreateConfig) *models.NetworkConfig {
	if cc.Config.NetworkDisabled {
		return nil
	}

	nc := &models.NetworkConfig{
		NetworkName: cc.HostConfig.NetworkMode.NetworkName(),
	}
	if cc.NetworkingConfig != nil {
		if es, ok := cc.NetworkingConfig.EndpointsConfig[nc.NetworkName]; ok {
			if es.IPAMConfig != nil {
				nc.Address = &es.IPAMConfig.IPv4Address
			}
		}
	}

	return nc
}

func (c *Container) imageExist(imageID string) (storeName string, err error) {
	// Call the storage port layer to determine if the image currently exist
	host, err := os.Hostname()
	if err != nil {
		return "", derr.NewBadRequestError(fmt.Errorf("container.ContainerCreate got unexpected error getting hostname"))
	}

	getParams := storage.NewGetImageParams().WithID(imageID).WithStoreName(host)
	if _, err := PortLayerClient().Storage.GetImage(getParams); err != nil {
		// If the image does not exist
		if _, isa := err.(*storage.GetImageNotFound); isa {
			// return error and "No such image" which the client looks for to determine if the image didn't exist
			return "", derr.NewRequestNotFoundError(fmt.Errorf("No such image: %s", imageID))
		}

		// If we get here, most likely something went wrong with the port layer API server
		return "", derr.NewErrorWithStatusCode(fmt.Errorf("Unknown error from the storage portlayer"),
			http.StatusInternalServerError)
	}

	return host, nil
}

func (c *Container) getImageMetadataFromStoragePL(image string) (*viccontainer.VicContainer, error) {
	// FIXME: This is a temporary workaround until we have a name resolution story.
	// Call imagec with -resolv parameter to learn the name of the vmdk and put it into in-memory map
	cmdArgs := []string{"-reference", image, "-resolv", "-standalone", "-destination", os.TempDir()}

	out, err := goexec.Command(Imagec, cmdArgs...).Output()
	if err != nil {
		log.Printf("%s exit code: %s", Imagec, err)
		return nil,
			derr.NewErrorWithStatusCode(fmt.Errorf("Container look up failed"),
				http.StatusInternalServerError)
	}
	var v1 struct {
		ID string `json:"id"`
		// https://github.com/docker/engine-api/blob/master/types/container/config.go
		Config container.Config `json:"config"`
	}
	if err := json.Unmarshal(out, &v1); err != nil {
		return nil,
			derr.NewErrorWithStatusCode(fmt.Errorf("Failed to unmarshall image history: %s", err),
				http.StatusInternalServerError)
	}
	log.Printf("v1 = %+v", v1)

	imageMetadata := &viccontainer.VicContainer{
		ID:     v1.ID,
		Config: &v1.Config,
	}

	return imageMetadata, nil
}

func (c *Container) getContainerConfigFromExecPL(name string) (*container.Config, error) {
	// Get an API client to the portlayer
	client := PortLayerClient()
	if client == nil {
		return nil,
			derr.NewErrorWithStatusCode(fmt.Errorf("container.ContainerCreate failed to create a portlayer client"),
				http.StatusInternalServerError)
	}

	//FIXME:  Handle size look up

	// Get the container info from the port layer
	plInfoParam := exec.NewContainerInfoParams().WithID(name)

	plContainerInfo, err := client.Exec.ContainerInfo(plInfoParam)
	if err != nil {
		if _, isa := err.(*exec.ContainerInfoNotFound); isa {
			return nil, derr.NewRequestNotFoundError(fmt.Errorf("No such container: %s", name))
		}

		// If we get here, most likely something went wrong with the port layer API server
		return nil,
			derr.NewErrorWithStatusCode(fmt.Errorf("Unknown error from the exec port layer"),
				http.StatusInternalServerError)
	}

	log.Printf("Container info from exec portlayer = %+v", plContainerInfo)
	// Transform the portlayer info to docker compatible data and return

	return nil, nil
}

// Test code

func TestAttachEnclosed(vc *viccontainer.VicContainer, ca *backend.ContainerAttachConfig) {
	vc.NewInputPipes()

	cmd := goexec.Command("/home/loc/go/src/github.com/test/test")

	inStream, outStream, _, err := ca.GetStreams()
	cmd.Stdout = vc.Stdout()
	cmd.Stdin = vc.Stdin()

	err = cmd.Start()
	if err != nil {
		log.Panic(err)
	}

	go func() {
		log.Printf("Copying conn to process stdin")
		_, err := io.Copy(vc.StdinPipe(), inStream)

		if err != nil {
			log.Printf("Got an error copying stdin - %+v\n", err)
		}
	}()
	go func() {
		log.Printf("Copying process stdout to conn")
		io.Copy(outStream, vc.StdoutPipe())
		log.Printf("Finished copying stdout\n")
	}()
}
