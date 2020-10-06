// +build !solution

package scheduler

import (
	"context"
	"time"

	"go.uber.org/zap"

	"gitlab.com/slon/shad-go/distbuild/pkg/api"
	"gitlab.com/slon/shad-go/distbuild/pkg/build"
)

var timeAfter = time.After

type PendingJob struct {
	Job      *api.JobSpec
	Finished chan struct{}
	Result   *api.JobResult
}

type Config struct {
	CacheTimeout time.Duration
	DepsTimeout  time.Duration
}

type Scheduler struct {
	l      *zap.Logger
	config Config

	globalQueue      chan *PendingJob
	firstLocalQueue  map[api.WorkerID]chan *PendingJob
	secondLocalQueue map[api.WorkerID]chan *PendingJob

	pickedJobs map[api.WorkerID]map[build.ID]*PendingJob //cache
}

func NewScheduler(l *zap.Logger, config Config) *Scheduler {
	s := Scheduler{l: l, config: config,
		globalQueue: make(chan *PendingJob, 100),

		pickedJobs: make(map[api.WorkerID]map[build.ID]*PendingJob),

		firstLocalQueue:  make(map[api.WorkerID]chan *PendingJob),
		secondLocalQueue: make(map[api.WorkerID]chan *PendingJob),
	}
	return &s
}

func (c *Scheduler) LocateArtifact(id build.ID) (api.WorkerID, bool) {
	panic("implement me")
}

func (c *Scheduler) RegisterWorker(workerID api.WorkerID) {
	c.pickedJobs[workerID] = make(map[build.ID]*PendingJob)

	c.firstLocalQueue[workerID] = make(chan *PendingJob, 100)
	c.secondLocalQueue[workerID] = make(chan *PendingJob, 100)
}

func (c *Scheduler) OnJobComplete(workerID api.WorkerID, jobID build.ID, res *api.JobResult) bool {
	if c.pickedJobs[workerID][jobID] == nil {
		c.pickedJobs[workerID][jobID] = &PendingJob{Finished: make(chan struct{}, 1)}
	}

	c.pickedJobs[workerID][jobID].Result = res
	c.pickedJobs[workerID][jobID].Finished <- struct{}{}

	return true
}

func (c *Scheduler) ScheduleJob(job *api.JobSpec) *PendingJob {
	pending := PendingJob{
		Job:      job,
		Finished: make(chan struct{}, 1),
		Result:   nil,
	}

	for workerID := range c.pickedJobs {
		for id := range c.pickedJobs[workerID] {
			if id == job.ID {
				c.firstLocalQueue[workerID] <- &pending
			}
		}
	}

	go func() {
		c2 := timeAfter(c.config.DepsTimeout)
		for workerID := range c.secondLocalQueue {
			flag := false
			for _, id := range job.Job.Deps {
				if _, ok := c.pickedJobs[workerID][id]; ok {
					flag = true
				}
			}
			if flag {
				c.secondLocalQueue[workerID] <- &pending
				return
			}
		}
		<-c2
		c.globalQueue <- &pending
	}()

	return &pending
}

func (c *Scheduler) PickJob(ctx context.Context, workerID api.WorkerID) *PendingJob {
	select {
	case <-ctx.Done():
		return nil
	case p := <-c.globalQueue:
		c.pickedJobs[workerID][p.Job.ID] = p
		return p
	case p := <-c.firstLocalQueue[workerID]:
		return p
	case p := <-c.secondLocalQueue[workerID]:
		c.pickedJobs[workerID][p.Job.ID] = p
		return p
	}
}
