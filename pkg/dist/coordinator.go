// +build !solution

package dist

import (
	"context"
	"fmt"
	"gitlab.com/slon/shad-go/distbuild/pkg/api"
	"gitlab.com/slon/shad-go/distbuild/pkg/build"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	"gitlab.com/slon/shad-go/distbuild/pkg/filecache"
	"gitlab.com/slon/shad-go/distbuild/pkg/scheduler"
)

type Coordinator struct {
	log       *zap.Logger
	filecache *filecache.Cache

	heartbeatHandler *api.HeartbeatHandler
	buildHandler     *api.BuildHandler
	filecacheHandler *filecache.Handler

	heartbeatService *HearbeatService
	buildService     *BuildService

	scheduler *scheduler.Scheduler

	artifacts   map[build.ID]api.WorkerID
	jobFinished map[build.ID]chan struct{}

	mu sync.RWMutex
}

type BuildService struct {
	coordinator *Coordinator
	pendingJobs map[build.ID]*scheduler.PendingJob
	jobResults  chan *api.JobResult

	uploaded map[build.ID]chan struct{}
}

func (b *BuildService) StartBuild(ctx context.Context, request *api.BuildRequest, w api.StatusWriter) error {
	graphID := build.NewID()
	b.uploaded[graphID] = make(chan struct{}, 1)

	var missingFiles []build.ID
	for missingFileID := range request.Graph.SourceFiles {
		missingFiles = append(missingFiles, missingFileID)
	}

	buildStarted := api.BuildStarted{ID: graphID, MissingFiles: missingFiles}
	err := w.Started(&buildStarted)

	<-b.uploaded[graphID]

	b.coordinator.log.Info("call writer Started")
	if err != nil {
		panic(err)
	}

	results := make(chan *api.JobResult, 20)

	jobs := build.TopSort(request.Graph.Jobs)
	for _, job := range jobs {
		//todo artifacts; cant add the here
		jobSpec := &api.JobSpec{Job: job, SourceFiles: request.Graph.SourceFiles}
		b.coordinator.log.Info(fmt.Sprintf("%s - job started", job.ID.String()))

		b.coordinator.mu.Lock()
		if b.coordinator.jobFinished[job.ID] == nil {
			b.coordinator.jobFinished[job.ID] = make(chan struct{})
		}
		b.coordinator.mu.Unlock()

		go b.ProcessJob(jobSpec, results)
	}

	for i := range jobs {
		b.coordinator.log.Info(fmt.Sprintf("waiting job %d for finishing\n", i))
		jobResult := <-results

		b.coordinator.log.Debug(string(jobResult.Stdout) + " RESULT STDOUT")

		b.coordinator.log.Info("trying to close")
		b.coordinator.mu.Lock()
		if !IsClosed(b.coordinator.jobFinished[jobResult.ID]) {
			b.coordinator.log.Info("closing")
			close(b.coordinator.jobFinished[jobResult.ID])
		}
		b.coordinator.mu.Unlock()

		statusUpdate := &api.StatusUpdate{
			JobFinished: jobResult,
		}
		b.coordinator.log.Info(fmt.Sprintf("job result updating"))
		fmt.Println(string(statusUpdate.JobFinished.Stdout))
		err = w.Updated(statusUpdate)
		if err != nil {
			panic(err)
		}
	}

	b.coordinator.log.Info(fmt.Sprintf("all jobs done"))

	statusUpdate := &api.StatusUpdate{
		BuildFinished: &api.BuildFinished{},
	}
	err = w.Updated(statusUpdate)
	if err != nil {
		panic(err)
	}
	return nil
}

func (b *BuildService) ProcessJob(job *api.JobSpec, results chan *api.JobResult) {
	// todo wait for all artifacts finished

	if job.Deps != nil {
		for _, dependency := range job.Deps {
			b.coordinator.log.Info("START WAITING ON JOBSINISHED: " + dependency.String())

			b.coordinator.mu.RLock()
			wait, ok := b.coordinator.jobFinished[dependency]
			if !ok {
				panic("dependency not parked")
			}
			b.coordinator.mu.RUnlock()
			<-wait
			b.coordinator.log.Info("ENDED WAITING ON JOBSINISHED")
		}
		job.Artifacts = b.coordinator.artifacts
	}

	pendingJob := b.coordinator.scheduler.ScheduleJob(job)
	b.pendingJobs[job.ID] = pendingJob
	b.coordinator.log.Info(fmt.Sprintf("waiting %s - job pending", job.ID.String()))
	<-pendingJob.Finished
	b.coordinator.log.Info(fmt.Sprintf("%s - job finished", job.ID.String()))
	results <- pendingJob.Result
}

func (b *BuildService) SignalBuild(ctx context.Context, buildID build.ID, signal *api.SignalRequest) (*api.SignalResponse, error) {
	b.uploaded[buildID] <- struct{}{}
	files, err := ioutil.ReadDir(b.coordinator.filecache.GetRoot())
	if err != nil {
		panic(err)
	}
	for _, f := range files {
		b.coordinator.log.Info(fmt.Sprintf("uploaded file %s", f.Name()))
	}
	return &api.SignalResponse{}, nil
}

type HearbeatService struct {
	coordinator       *Coordinator
	registeredWorkers map[api.WorkerID]bool
}

func IsClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
	}
	return false
}

func (h *HearbeatService) Heartbeat(ctx context.Context, req *api.HeartbeatRequest) (*api.HeartbeatResponse, error) {
	if !h.registeredWorkers[req.WorkerID] {
		h.coordinator.log.Info("registering worker")
		h.coordinator.scheduler.RegisterWorker(req.WorkerID)
		h.registeredWorkers[req.WorkerID] = true
	}
	if len(req.FinishedJob) > 0 {
		h.coordinator.log.Info("received finished job")
		for i := range req.FinishedJob {
			h.coordinator.log.Info("jobResult iteration " + req.FinishedJob[i].ID.String())
			h.coordinator.scheduler.OnJobComplete(req.WorkerID, req.FinishedJob[i].ID, &req.FinishedJob[i])
			h.coordinator.log.Info("on job completed " + req.FinishedJob[i].ID.String())
			if req.AddedArtifacts == nil {
				panic("nil AddedArtifacts")
			}
			for _, artifact := range req.AddedArtifacts {
				h.coordinator.log.Info("added artifacts iteration")
				h.coordinator.artifacts[artifact] = req.WorkerID
			}
		}
	}

	if req.FreeSlots > 0 {
		pendingJob := h.coordinator.scheduler.PickJob(ctx, req.WorkerID)

		if pendingJob == nil {
			return &api.HeartbeatResponse{}, nil //todo return finished error
		}

		sourceFiles := pendingJob.Job.SourceFiles

		if sourceFiles != nil {
			filecacheClient := filecache.NewClient(h.coordinator.log, req.WorkerID.String())
			for fileID := range sourceFiles {
				path, unlock, err := h.coordinator.filecache.Get(fileID)
				if err != nil {
					panic(err)
				}
				err = filecacheClient.Upload(ctx, fileID, path)
				if err != nil {
					panic(err)
				}
				unlock()
			}
		}

		jobsToRun := make(map[build.ID]api.JobSpec)
		jobsToRun[pendingJob.Job.ID] = *pendingJob.Job
		response := &api.HeartbeatResponse{JobsToRun: jobsToRun}
		return response, nil
	}
	return &api.HeartbeatResponse{}, nil
}

var defaultConfig = scheduler.Config{
	CacheTimeout: time.Millisecond * 10,
	DepsTimeout:  time.Millisecond * 100,
}

func NewCoordinator(
	log *zap.Logger,
	fileCache *filecache.Cache,
) *Coordinator {
	jobFinished := make(map[build.ID]chan struct{})

	heartbeatService := &HearbeatService{registeredWorkers: make(map[api.WorkerID]bool)}
	buildService := &BuildService{uploaded: make(map[build.ID]chan struct{}), pendingJobs: make(map[build.ID]*scheduler.PendingJob), jobResults: make(chan *api.JobResult, 20)}

	hearbeatHandler := api.NewHeartbeatHandler(log.Named("heartbeat"), heartbeatService)
	buildHandler := api.NewBuildService(log.Named("build"), buildService)

	filecacheHandler := filecache.NewHandler(log.Named("filecache"), fileCache)

	scheduler := scheduler.NewScheduler(log.Named("scheduler"), defaultConfig)

	coordinator := &Coordinator{
		log:              log,
		filecache:        fileCache,
		heartbeatHandler: hearbeatHandler,
		buildHandler:     buildHandler,
		filecacheHandler: filecacheHandler,
		heartbeatService: heartbeatService,
		buildService:     buildService,
		scheduler:        scheduler,
		artifacts:        make(map[build.ID]api.WorkerID),
		jobFinished:      jobFinished,
		mu:               sync.RWMutex{},
	}
	heartbeatService.coordinator = coordinator
	buildService.coordinator = coordinator
	return coordinator
}

func (c *Coordinator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mux := http.NewServeMux()

	c.buildHandler.Register(mux)
	c.heartbeatHandler.Register(mux)
	c.filecacheHandler.Register(mux)

	mux.ServeHTTP(w, r)
}
