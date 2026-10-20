package controlplane_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"komrad/internal/controlplane"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var ec2Now = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

// fakeInstances answers for the instances it holds the way EC2 filters them by tag and state.
type fakeInstances struct {
	instances []types.Instance
	runErr    error
	started   []string
	stopped   []string
}

func (f *fakeInstances) DescribeInstances(_ context.Context, input *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	var matched []types.Instance
	for _, instance := range f.instances {
		if isFilteredIn(instance, input.Filters) {
			matched = append(matched, instance)
		}
	}

	return &ec2.DescribeInstancesOutput{Reservations: []types.Reservation{{Instances: matched}}}, nil
}

func (f *fakeInstances) StartInstances(_ context.Context, input *ec2.StartInstancesInput, _ ...func(*ec2.Options)) (*ec2.StartInstancesOutput, error) {
	if f.runErr != nil {
		return nil, f.runErr
	}
	f.started = append(f.started, input.InstanceIds...)

	return &ec2.StartInstancesOutput{}, nil
}

func (f *fakeInstances) StopInstances(_ context.Context, input *ec2.StopInstancesInput, _ ...func(*ec2.Options)) (*ec2.StopInstancesOutput, error) {
	if f.runErr != nil {
		return nil, f.runErr
	}
	f.stopped = append(f.stopped, input.InstanceIds...)

	return &ec2.StopInstancesOutput{}, nil
}

func isFilteredIn(instance types.Instance, filters []types.Filter) bool {
	for _, filter := range filters {
		var value string
		switch name := aws.ToString(filter.Name); name {
		case "instance-state-name":
			value = string(instance.State.Name)
		default:
			for _, tag := range instance.Tags {
				if "tag:"+aws.ToString(tag.Key) == name {
					value = aws.ToString(tag.Value)
				}
			}
		}
		if !slices.Contains(filter.Values, value) {
			return false
		}
	}

	return true
}

type newInstanceParams struct {
	ID       string
	Stack    string
	Engine   string
	State    types.InstanceStateName
	Launched time.Duration
}

// newInstance is an instance in the given state that launched the given time before ec2Now.
func newInstance(params newInstanceParams) types.Instance {
	return types.Instance{
		InstanceId: aws.String(params.ID),
		State:      &types.InstanceState{Name: params.State},
		LaunchTime: aws.Time(ec2Now.Add(-params.Launched)),
		Tags: []types.Tag{
			{Key: aws.String(controlplane.StackTag), Value: aws.String(params.Stack)},
			{Key: aws.String(controlplane.EngineTag), Value: aws.String(params.Engine)},
		},
	}
}

func newEC2Runner(instances *fakeInstances, now *time.Time) *controlplane.EC2Runner {
	return controlplane.NewEC2Runner(controlplane.NewEC2RunnerParams{
		Instances: instances,
		Stack:     "komrad",
		Now:       func() time.Time { return *now },
	})
}

// TestEC2RunnerState covers reading an engine's state from its instance.
func TestEC2RunnerState(t *testing.T) {
	tests := []struct {
		name      string
		instances []types.Instance
		wantErr   string
		want      controlplane.EngineState
	}{
		{
			name:      "no instance runs the engine",
			instances: nil,
			wantErr:   "no instance runs engine harry",
		},
		{
			name: "the engine's instance in another stack",
			instances: []types.Instance{
				newInstance(newInstanceParams{ID: "i-other", Stack: "staging", Engine: "harry", State: types.InstanceStateNameRunning, Launched: time.Hour}),
			},
			wantErr: "no instance runs engine harry",
		},
		{
			name: "a pending instance",
			instances: []types.Instance{
				newInstance(newInstanceParams{ID: "i-harry", Stack: "komrad", Engine: "harry", State: types.InstanceStateNamePending}),
			},
			want: controlplane.EngineStarting,
		},
		{
			name: "a running instance still booting",
			instances: []types.Instance{
				newInstance(newInstanceParams{ID: "i-harry", Stack: "komrad", Engine: "harry", State: types.InstanceStateNameRunning, Launched: time.Minute}),
			},
			want: controlplane.EngineStarting,
		},
		{
			name: "a stopping instance",
			instances: []types.Instance{
				newInstance(newInstanceParams{ID: "i-harry", Stack: "komrad", Engine: "harry", State: types.InstanceStateNameStopping, Launched: time.Hour}),
			},
			want: controlplane.EngineStopping,
		},
		{
			name: "a stopped instance",
			instances: []types.Instance{
				newInstance(newInstanceParams{ID: "i-harry", Stack: "komrad", Engine: "harry", State: types.InstanceStateNameStopped, Launched: time.Hour}),
			},
			want: controlplane.EngineStopped,
		},
		{
			name: "a replaced instance shutting down beside its replacement",
			instances: []types.Instance{
				newInstance(newInstanceParams{ID: "i-old", Stack: "komrad", Engine: "harry", State: types.InstanceStateNameShuttingDown, Launched: 48 * time.Hour}),
				newInstance(newInstanceParams{ID: "i-new", Stack: "komrad", Engine: "harry", State: types.InstanceStateNameRunning, Launched: time.Hour}),
			},
			want: controlplane.EngineRunning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := ec2Now
			runner := newEC2Runner(&fakeInstances{instances: tt.instances}, &now)

			state, err := runner.State(t.Context(), "harry")
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, state)
		})
	}
}

// TestEC2RunnerStartAndStop covers which instances starting and stopping an engine moves.
func TestEC2RunnerStartAndStop(t *testing.T) {
	tests := []struct {
		name        string
		state       types.InstanceStateName
		stop        bool
		runErr      error
		wantErr     string
		wantStarted []string
		wantStopped []string
	}{
		{
			name:    "starting a stopping engine",
			state:   types.InstanceStateNameStopping,
			wantErr: "the engine is stopping",
		},
		{
			name:    "stopping a pending engine",
			state:   types.InstanceStateNamePending,
			stop:    true,
			wantErr: "the engine is starting",
		},
		{
			name:    "EC2 refusing the start",
			state:   types.InstanceStateNameStopped,
			runErr:  errors.New("insufficient capacity"),
			wantErr: "insufficient capacity",
		},
		{
			name:  "starting a running engine",
			state: types.InstanceStateNameRunning,
		},
		{
			name:  "stopping a stopped engine",
			state: types.InstanceStateNameStopped,
			stop:  true,
		},
		{
			name:        "starting a stopped engine",
			state:       types.InstanceStateNameStopped,
			wantStarted: []string{"i-harry"},
		},
		{
			name:        "stopping a running engine",
			state:       types.InstanceStateNameRunning,
			stop:        true,
			wantStopped: []string{"i-harry"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := ec2Now
			instances := &fakeInstances{
				instances: []types.Instance{
					newInstance(newInstanceParams{ID: "i-harry", Stack: "komrad", Engine: "harry", State: tt.state, Launched: time.Hour}),
				},
				runErr: tt.runErr,
			}
			runner := newEC2Runner(instances, &now)

			run := runner.Start
			if tt.stop {
				run = runner.Stop
			}
			err := run(t.Context(), "harry")
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantStarted, instances.started)
			assert.Equal(t, tt.wantStopped, instances.stopped)
		})
	}
}

// TestEC2RunnerReadsAJustStartedEngineAsStarting covers EC2 describing an instance it was just
// told to start as still stopped.
func TestEC2RunnerReadsAJustStartedEngineAsStarting(t *testing.T) {
	now := ec2Now
	runner := newEC2Runner(&fakeInstances{
		instances: []types.Instance{
			newInstance(newInstanceParams{ID: "i-harry", Stack: "komrad", Engine: "harry", State: types.InstanceStateNameStopped, Launched: time.Hour}),
		},
	}, &now)
	require.NoError(t, runner.Start(t.Context(), "harry"))

	state, err := runner.State(t.Context(), "harry")
	require.NoError(t, err)
	assert.Equal(t, controlplane.EngineStarting, state)

	now = now.Add(2 * time.Minute)
	state, err = runner.State(t.Context(), "harry")
	require.NoError(t, err)
	assert.Equal(t, controlplane.EngineStopped, state)
}
