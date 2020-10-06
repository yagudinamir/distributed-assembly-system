// +build !solution

package worker

import (
	"bytes"
	"context"
	"fmt"
	"gitlab.com/slon/shad-go/distbuild/pkg/api"
	"gitlab.com/slon/shad-go/distbuild/pkg/artifact"
	"gitlab.com/slon/shad-go/distbuild/pkg/build"
	"gitlab.com/slon/shad-go/distbuild/pkg/filecache"
	"go.uber.org/zap"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
)

type Worker struct {
	workerID            api.WorkerID
	coordinatorEndpoint string
	log                 *zap.Logger
	fileCache           *filecache.Cache
	artifacts           *artifact.Cache

	runningJobs []build.ID
	freeSlots   int

	jobResults chan *api.JobResult

	jobResultCache map[build.ID]*api.JobResult
}

func New(
	workerID api.WorkerID,
	coordinatorEndpoint string,
	log *zap.Logger,
	fileCache *filecache.Cache,
	artifacts *artifact.Cache,
) *Worker {
	return &Worker{
		workerID:            workerID,
		coordinatorEndpoint: coordinatorEndpoint,
		log:                 log,
		fileCache:           fileCache,
		artifacts:           artifacts,

		freeSlots: 1,

		jobResults: make(chan *api.JobResult, 10),

		jobResultCache: make(map[build.ID]*api.JobResult),
	}
}

//todo create artifact

func (w *Worker) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	//todo run for Artifact & FileCache handling
	filecacheHandler := filecache.NewHandler(w.log.Named("filecache"), w.fileCache)
	artifactHandler := artifact.NewHandler(w.log.Named("artifact"), w.artifacts)
	mux := http.NewServeMux()
	filecacheHandler.Register(mux)
	artifactHandler.Register(mux)
	mux.ServeHTTP(rw, r)
}

func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("running worker")
	heartbeatClient := api.NewHeartbeatClient(w.log, w.coordinatorEndpoint)

	for {
		var finishedJobs []api.JobResult
		var addedArtifacts []build.ID

		for {
			var result *api.JobResult
			finished := false
			select {
			case result = <-w.jobResults:
				finishedJobs = append(finishedJobs, *result)

				addedArtifacts = append(addedArtifacts, result.ID)

				w.jobResultCache[result.ID] = result
				w.freeSlots++
				if w.freeSlots > 1 {
					panic("free slots > 1")
				}
			default:
				finished = true
			}
			if finished {
				break
			}
		}

		hearbeatRequest := &api.HeartbeatRequest{
			WorkerID:       w.workerID,
			RunningJobs:    w.runningJobs,
			FreeSlots:      w.freeSlots,
			FinishedJob:    finishedJobs,
			AddedArtifacts: addedArtifacts,
		}
		heartbeatResponse, err := heartbeatClient.Heartbeat(ctx, hearbeatRequest)
		if err != nil {
			return err
		}

		for jobID := range heartbeatResponse.JobsToRun {
			w.freeSlots--
			if w.freeSlots < 0 {
				panic("negative free slots")
			}
			if jobResult, ok := w.jobResultCache[heartbeatResponse.JobsToRun[jobID].ID]; ok {
				w.jobResults <- jobResult
			} else {
				go w.ProcessJob(ctx, heartbeatResponse.JobsToRun[jobID])
			}
		}
	}
}

func (w *Worker) ProcessJob(ctx context.Context, spec api.JobSpec) {
	if spec.SourceFiles != nil {
		root := w.fileCache.GetRoot()
		for fileID := range spec.SourceFiles {
			path, unlock, err := w.fileCache.Get(fileID)
			if err != nil {
				panic(err)
			}
			_ = os.MkdirAll(filepath.Dir(filepath.Join(root, spec.SourceFiles[fileID])), 0777)
			_ = os.Rename(path, filepath.Join(root, spec.SourceFiles[fileID]))
			unlock()
		}
	}

	deps := make(map[build.ID]string)

	if spec.Deps != nil {
		for _, artifactID := range spec.Deps {
			artifactWorker := spec.Artifacts[artifactID]

			if artifactWorker != w.workerID {
				_ = artifact.Download(ctx, artifactWorker.String(), w.artifacts, artifactID)
			}
			path, unlock, err := w.artifacts.Get(artifactID)
			if err != nil {
				panic(err)
			}
			deps[artifactID] = path
			unlock()
		}
	}

	cmds := spec.Job.Cmds
	var stdouts [][]byte
	var stderrs [][]byte
	var globalErr error = nil

	outputDir, commit, _, er := w.artifacts.Create(spec.ID)
	if er != nil {
		panic(er)
	}

	for _, cmd := range cmds {
		ctx := build.JobContext{ //todo
			SourceDir: w.fileCache.GetRoot(),
			OutputDir: outputDir,
			Deps:      deps,
		}
		rendered, err := cmd.Render(ctx)
		if err != nil {
			panic(err)
		}

		if rendered.Exec != nil {
			stdout, stderr, err := execute(rendered)
			stdouts = append(stdouts, stdout)
			stderrs = append(stderrs, stderr)
			if err != nil {
				globalErr = err
			}
		} else if rendered.CatTemplate != "" {
			err := ioutil.WriteFile(rendered.CatOutput, []byte(rendered.CatTemplate), 0666)
			if err != nil {
				panic(err)
			}
		} else {
			panic("execute")
		}
	}

	var jobResult api.JobResult
	jobResult.ID = spec.Job.ID
	if globalErr != nil {
		error := globalErr.Error()
		jobResult.Error = &error
		jobResult.ExitCode = 1
	} else {
		jobResult.ExitCode = 0
	}

	sep := []byte("")

	jobResult.Stdout = bytes.Join(stdouts, sep)
	jobResult.Stderr = bytes.Join(stderrs, sep)

	w.log.Debug(fmt.Sprintf("job result stdout: %v", string(jobResult.Stdout)))
	w.log.Debug(fmt.Sprintf("job result stderr: %v", string(jobResult.Stderr)))

	err := commit()
	if err != nil {
		panic(err)
	}

	w.jobResults <- &jobResult
}

func execute(buildCmd *build.Cmd) ([]byte, []byte, error) {
	cmd := exec.Command(buildCmd.Exec[0], buildCmd.Exec[1:]...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = buildCmd.Environ
	cmd.Dir = buildCmd.WorkingDirectory

	err := cmd.Run()

	return stdout.Bytes(), stderr.Bytes(), err
}
