package paneltest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"komrad/proto/gen/agent/v1/agentv1connect"
	"komrad/proto/gen/forge/v1/forgev1connect"
	"komrad/proto/gen/hook/v1/hookv1connect"
	"komrad/proto/gen/log/v1/logv1connect"
	"komrad/proto/gen/loop/v1/loopv1connect"
	"komrad/proto/gen/pullrequest/v1/pullrequestv1connect"
	schedulev1connect "komrad/proto/gen/schedule/v1/schedulev1connect"
	"komrad/proto/gen/task/v1/taskv1connect"
	"komrad/proto/gen/workflow/v1/workflowv1connect"
	"komrad/proto/gen/worktree/v1/worktreev1connect"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Remote is the server clients a panel under test sends its requests through.
type Remote struct {
	Agents       agentv1connect.AgentServiceClient
	Workflows    workflowv1connect.WorkflowServiceClient
	Schedules    schedulev1connect.ScheduleServiceClient
	Hooks        hookv1connect.HookServiceClient
	Worktrees    worktreev1connect.WorktreeServiceClient
	Logs         logv1connect.LogServiceClient
	Forges       forgev1connect.ForgeServiceClient
	PullRequests pullrequestv1connect.PullRequestServiceClient
	Loops        loopv1connect.LoopServiceClient
	Tasks        taskv1connect.TaskServiceClient
}

// Request is one request the panel sent, by the procedure it called.
type Request struct {
	Procedure string
	Msg       proto.Message
}

// AnswerFunc answers a request the panel sent in the server's place.
type AnswerFunc func(req connect.AnyRequest) (connect.AnyResponse, error)

// AnswerWith answers every request with msg.
func AnswerWith[T any](msg *T) AnswerFunc {
	return func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(msg), nil
	}
}

// RefuseWith refuses every request with code, giving reason.
func RefuseWith(code connect.Code, reason string) AnswerFunc {
	return func(connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, connect.NewError(code, errors.New(reason))
	}
}

type recorder struct {
	mu      sync.Mutex
	seen    []Request
	answers map[string]AnswerFunc
	gate    *gate
}

// newRemote serves every service from a recording server that answers each request empty unless
// told otherwise.
func newRemote(t *testing.T, recorder *recorder) *Remote {
	t.Helper()

	interceptors := connect.WithInterceptors(recorder)

	mux := http.NewServeMux()
	mux.Handle(agentv1connect.NewAgentServiceHandler(agentv1connect.UnimplementedAgentServiceHandler{}, interceptors))
	mux.Handle(workflowv1connect.NewWorkflowServiceHandler(workflowv1connect.UnimplementedWorkflowServiceHandler{}, interceptors))
	mux.Handle(schedulev1connect.NewScheduleServiceHandler(schedulev1connect.UnimplementedScheduleServiceHandler{}, interceptors))
	mux.Handle(hookv1connect.NewHookServiceHandler(hookv1connect.UnimplementedHookServiceHandler{}, interceptors))
	mux.Handle(worktreev1connect.NewWorktreeServiceHandler(worktreev1connect.UnimplementedWorktreeServiceHandler{}, interceptors))
	mux.Handle(logv1connect.NewLogServiceHandler(logv1connect.UnimplementedLogServiceHandler{}, interceptors))
	mux.Handle(forgev1connect.NewForgeServiceHandler(forgev1connect.UnimplementedForgeServiceHandler{}, interceptors))
	mux.Handle(pullrequestv1connect.NewPullRequestServiceHandler(pullrequestv1connect.UnimplementedPullRequestServiceHandler{}, interceptors))
	mux.Handle(loopv1connect.NewLoopServiceHandler(loopv1connect.UnimplementedLoopServiceHandler{}, interceptors))
	mux.Handle(taskv1connect.NewTaskServiceHandler(taskv1connect.UnimplementedTaskServiceHandler{}, interceptors))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return &Remote{
		Agents:       agentv1connect.NewAgentServiceClient(server.Client(), server.URL),
		Workflows:    workflowv1connect.NewWorkflowServiceClient(server.Client(), server.URL),
		Schedules:    schedulev1connect.NewScheduleServiceClient(server.Client(), server.URL),
		Hooks:        hookv1connect.NewHookServiceClient(server.Client(), server.URL),
		Worktrees:    worktreev1connect.NewWorktreeServiceClient(server.Client(), server.URL),
		Logs:         logv1connect.NewLogServiceClient(server.Client(), server.URL),
		Forges:       forgev1connect.NewForgeServiceClient(server.Client(), server.URL),
		PullRequests: pullrequestv1connect.NewPullRequestServiceClient(server.Client(), server.URL),
		Loops:        loopv1connect.NewLoopServiceClient(server.Client(), server.URL),
		Tasks:        taskv1connect.NewTaskServiceClient(server.Client(), server.URL),
	}
}

func (r *recorder) requests() []Request {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.seen)
}

func (r *recorder) answer(procedure string, answer AnswerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.answers[procedure] = answer
}

func (r *recorder) WrapUnary(connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		msg, ok := req.Any().(proto.Message)
		if !ok {
			return nil, fmt.Errorf("%s sent a request that is not a proto message", req.Spec().Procedure)
		}

		r.mu.Lock()
		r.seen = append(r.seen, Request{
			Procedure: req.Spec().Procedure,
			Msg:       msg,
		})
		answer := r.answers[req.Spec().Procedure]
		r.mu.Unlock()

		r.gate.wait(ctx, req.Spec().Procedure)

		if answer != nil {
			return answer(req)
		}

		return connect.NewResponse(&emptypb.Empty{}), nil
	}
}

func (r *recorder) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (r *recorder) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		r.mu.Lock()
		r.seen = append(r.seen, Request{Procedure: conn.Spec().Procedure})
		r.mu.Unlock()

		return next(ctx, conn)
	}
}
