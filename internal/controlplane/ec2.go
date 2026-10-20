package controlplane

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

const (
	// StackTag names the stack an engine's instance belongs to.
	StackTag = "KomradStack"
	// EngineTag names the engine an instance runs.
	EngineTag = "KomradEngine"
)

const (
	// bootTime is how long a running instance's engine takes to answer after it launched.
	bootTime = 5 * time.Minute
	// startLag is how long EC2 may still describe an instance as stopped after starting it.
	startLag = time.Minute
)

// instanceController is the part of EC2 the EC2 runner calls.
type instanceController interface {
	DescribeInstances(ctx context.Context, input *ec2.DescribeInstancesInput, opts ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	StartInstances(ctx context.Context, input *ec2.StartInstancesInput, opts ...func(*ec2.Options)) (*ec2.StartInstancesOutput, error)
	StopInstances(ctx context.Context, input *ec2.StopInstancesInput, opts ...func(*ec2.Options)) (*ec2.StopInstancesOutput, error)
}

// NewEC2RunnerParams is what an EC2 runner is built from.
type NewEC2RunnerParams struct {
	Instances instanceController
	Stack     string
	Now       func() time.Time
}

// EC2Runner starts and stops engines that each run on an EC2 instance tagged with their stack
// and name.
type EC2Runner struct {
	instances instanceController
	stack     string
	now       func() time.Time
	mu        sync.Mutex
	// started is when each engine was last started.
	started map[string]time.Time
}

// NewEC2Runner builds a runner over the stack's instances.
func NewEC2Runner(params NewEC2RunnerParams) *EC2Runner {
	return &EC2Runner{
		instances: params.Instances,
		stack:     params.Stack,
		now:       params.Now,
		started:   make(map[string]time.Time),
	}
}

// Start starts the engine's instance unless it is already running or starting.
func (e *EC2Runner) Start(ctx context.Context, engine string) error {
	instance, err := e.findInstance(ctx, engine)
	if err != nil {
		return err
	}

	switch instance.State.Name {
	case types.InstanceStateNamePending, types.InstanceStateNameRunning:
		return nil
	case types.InstanceStateNameStopping, types.InstanceStateNameShuttingDown, types.InstanceStateNameTerminated:
		return errors.New("the engine is stopping, so start it once that finishes")
	case types.InstanceStateNameStopped:
	}

	if _, err := e.instances.StartInstances(ctx, &ec2.StartInstancesInput{InstanceIds: []string{aws.ToString(instance.InstanceId)}}); err != nil {
		return fmt.Errorf("failed to start the instance for engine %s: %w", engine, err)
	}

	e.mu.Lock()
	e.started[engine] = e.now()
	e.mu.Unlock()

	return nil
}

// Stop stops the engine's instance unless it is already stopped or stopping.
func (e *EC2Runner) Stop(ctx context.Context, engine string) error {
	instance, err := e.findInstance(ctx, engine)
	if err != nil {
		return err
	}

	switch instance.State.Name {
	case types.InstanceStateNameStopped, types.InstanceStateNameStopping, types.InstanceStateNameShuttingDown, types.InstanceStateNameTerminated:
		return nil
	case types.InstanceStateNamePending:
		return errors.New("the engine is starting, so stop it once that finishes")
	case types.InstanceStateNameRunning:
	}

	if _, err := e.instances.StopInstances(ctx, &ec2.StopInstancesInput{InstanceIds: []string{aws.ToString(instance.InstanceId)}}); err != nil {
		return fmt.Errorf("failed to stop the instance for engine %s: %w", engine, err)
	}

	e.mu.Lock()
	delete(e.started, engine)
	e.mu.Unlock()

	return nil
}

// State is the engine's instance's state, read as starting while a running instance boots and
// while EC2 still describes one just started as stopped.
func (e *EC2Runner) State(ctx context.Context, engine string) (EngineState, error) {
	instance, err := e.findInstance(ctx, engine)
	if err != nil {
		return "", err
	}

	now := e.now()
	switch instance.State.Name {
	case types.InstanceStateNamePending:
		return EngineStarting, nil
	case types.InstanceStateNameRunning:
		if now.Sub(aws.ToTime(instance.LaunchTime)) < bootTime {
			return EngineStarting, nil
		}

		return EngineRunning, nil
	case types.InstanceStateNameStopping, types.InstanceStateNameShuttingDown:
		return EngineStopping, nil
	case types.InstanceStateNameStopped, types.InstanceStateNameTerminated:
	}

	e.mu.Lock()
	started, wasStarted := e.started[engine]
	e.mu.Unlock()
	if wasStarted && now.Sub(started) < startLag {
		return EngineStarting, nil
	}

	return EngineStopped, nil
}

// findInstance is the newest instance tagged with the stack and the engine that is not terminated.
func (e *EC2Runner) findInstance(ctx context.Context, engine string) (*types.Instance, error) {
	output, err := e.instances.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: []types.Filter{
			{Name: aws.String("tag:" + StackTag), Values: []string{e.stack}},
			{Name: aws.String("tag:" + EngineTag), Values: []string{engine}},
			{Name: aws.String("instance-state-name"), Values: []string{
				string(types.InstanceStateNamePending),
				string(types.InstanceStateNameRunning),
				string(types.InstanceStateNameStopping),
				string(types.InstanceStateNameStopped),
				string(types.InstanceStateNameShuttingDown),
			}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find the instance for engine %s: %w", engine, err)
	}

	var newest *types.Instance
	for _, reservation := range output.Reservations {
		for i := range reservation.Instances {
			instance := &reservation.Instances[i]
			if newest == nil || aws.ToTime(instance.LaunchTime).After(aws.ToTime(newest.LaunchTime)) {
				newest = instance
			}
		}
	}
	if newest == nil || newest.State == nil {
		return nil, fmt.Errorf("no instance runs engine %s in stack %s", engine, e.stack)
	}

	return newest, nil
}
