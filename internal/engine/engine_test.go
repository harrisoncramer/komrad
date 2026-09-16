package engine_test

import (
	"strings"
	"testing"
	"time"

	"weave/internal/engine"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsRemoteIsWhetherADestinationWasGiven covers the zero value meaning a local engine.
func TestIsRemoteIsWhetherADestinationWasGiven(t *testing.T) {
	t.Parallel()

	assert.False(t, engine.Remote{}.IsRemote())
	assert.False(t, engine.Remote{Destination: "  "}.IsRemote())
	assert.True(t, engine.Remote{Destination: "ubuntu@host"}.IsRemote())
}

// TestAttachArgvIsEmptyForALocalEngine covers the caller being able to ask without checking first.
func TestAttachArgvIsEmptyForALocalEngine(t *testing.T) {
	t.Parallel()

	assert.Nil(t, engine.Remote{Socket: "weave", Session: "weave"}.AttachArgv("%9"))
}

// TestAttachArgvIsEmptyWithoutAPane covers a stage asked to show nothing.
func TestAttachArgvIsEmptyWithoutAPane(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{Destination: "ubuntu@host", Socket: "weave", Session: "weave"}

	assert.Nil(t, remote.AttachArgv(""))
	assert.Nil(t, remote.AttachArgv("   "))
}

// TestAttachArgvRunsSshWithOneScript covers the shape the stage pane is respawned with.
func TestAttachArgvRunsSshWithOneScript(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{Destination: "ubuntu@host", Socket: "weave", Session: "weave"}

	argv := remote.AttachArgv("%9")
	require.Len(t, argv, 4)
	assert.Equal(t, []string{"ssh", "-t", "ubuntu@host"}, argv[:3])
}

// TestAttachArgvPointsAtAViewSessionOfItsOwn covers one session per staged agent, which is what
// keeps two stage panes from resizing one another.
func TestAttachArgvPointsAtAViewSessionOfItsOwn(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{Destination: "ubuntu@host", Socket: "weave", Session: "weave"}

	script := remote.AttachArgv("%9")[3]
	assert.Contains(t, script, "new-session -d -s 'view-9' -t 'weave'")
	assert.Contains(t, script, "exec tmux -L 'weave' attach -t 'view-9'")
	assert.NotEqual(t, engine.ViewSession("%9"), engine.ViewSession("%10"))
}

// TestAttachScriptSilencesTheRemoteStatusBar covers the nested tmux drawing its own chrome
// inside the stage pane when it is left on.
func TestAttachScriptSilencesTheRemoteStatusBar(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{Destination: "ubuntu@host", Socket: "weave", Session: "weave"}

	assert.Contains(t, remote.AttachArgv("%9")[3], "set-option -t 'view-9' status off")
}

// TestAttachScriptResolvesTheWindowOnTheEngine covers the panel knowing an agent by its pane
// while only the engine can say which window holds it.
func TestAttachScriptResolvesTheWindowOnTheEngine(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{Destination: "ubuntu@host", Socket: "weave", Session: "weave"}

	script := remote.AttachArgv("%9")[3]
	assert.Contains(t, script, "display-message -p -t '%9' '#{window_id}'")
	assert.Contains(t, script, "select-window -t 'view-9':$w")
}

// TestAttachScriptToleratesAViewSessionThatAlreadyExists covers the stage re-running this every
// time the cursor comes back to the same agent.
func TestAttachScriptToleratesAViewSessionThatAlreadyExists(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{Destination: "ubuntu@host", Socket: "weave", Session: "weave"}

	assert.Contains(t, remote.AttachArgv("%9")[3], "|| true")
}

// TestAttachScriptQuotesWhatItInterpolates covers a socket or session name the far side's shell
// would otherwise split or expand.
func TestAttachScriptQuotesWhatItInterpolates(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{
		Destination: "ubuntu@host",
		Socket:      "we ave",
		Session:     "it's",
	}

	script := remote.AttachArgv("%9")[3]
	assert.Contains(t, script, `tmux -L 'we ave'`)
	assert.Contains(t, script, `-t 'it'\''s'`)
	assert.NotContains(t, strings.ReplaceAll(script, `'\''`, ""), "it's")
}

// TestStringNamesTheEngine covers what a log line or an error says about where agents are.
func TestStringNamesTheEngine(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "local", engine.Remote{}.String())
	assert.Equal(t,
		"ubuntu@host (tmux -L weave)",
		engine.Remote{Destination: "ubuntu@host", Socket: "weave"}.String())
}

// TestTunnelArgvForwardsTheEnginesAddress covers the forward ssh is asked for.
func TestTunnelArgvForwardsTheEnginesAddress(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{Destination: "weave@host", Socket: "weave", Session: "weave"}

	argv := remote.TunnelArgv("127.0.0.1:54321", "127.0.0.1:8787")

	assert.Equal(t, "ssh", argv[0])
	assert.Contains(t, argv, "-N")
	assert.Contains(t, argv, "127.0.0.1:54321:127.0.0.1:8787")
	assert.Equal(t, "weave@host", argv[len(argv)-1])
}

// TestTunnelArgvFailsRatherThanOpeningAUselessSession covers ssh otherwise staying up with no
// forward when the port is taken, which would look like a working tunnel that carries nothing.
func TestTunnelArgvFailsRatherThanOpeningAUselessSession(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{Destination: "weave@host"}

	assert.Contains(t, remote.TunnelArgv("127.0.0.1:1", "127.0.0.1:8787"), "ExitOnForwardFailure=yes")
}

// TestDialRefusesALocalEngine covers the caller being told rather than an ssh to nowhere.
func TestDialRefusesALocalEngine(t *testing.T) {
	t.Parallel()

	tunnel, err := engine.Remote{}.Dial(t.Context(), "127.0.0.1:8787", time.Second)

	require.Error(t, err)
	assert.Nil(t, tunnel)
}

// TestClosingANilTunnelIsFine covers the panel's exit path running whether or not one was opened.
func TestClosingANilTunnelIsFine(t *testing.T) {
	t.Parallel()

	var tunnel *engine.Tunnel

	assert.NoError(t, tunnel.Close())
}

// TestTunnelPortIsStableForAnEngine covers a tunnel that outlived its panel being found again
// rather than leaked, which needs the port to be the same every time.
func TestTunnelPortIsStableForAnEngine(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{Destination: "weave@host"}

	assert.Equal(t, remote.TunnelPort("127.0.0.1:8787"), remote.TunnelPort("127.0.0.1:8787"))
}

// TestTunnelPortDiffersBetweenEngines covers two engines driven from one machine not fighting
// over the same forward.
func TestTunnelPortDiffersBetweenEngines(t *testing.T) {
	t.Parallel()

	first := engine.Remote{Destination: "weave@one"}.TunnelPort("127.0.0.1:8787")
	second := engine.Remote{Destination: "weave@two"}.TunnelPort("127.0.0.1:8787")
	other := engine.Remote{Destination: "weave@one"}.TunnelPort("127.0.0.1:9999")

	assert.NotEqual(t, first, second)
	assert.NotEqual(t, first, other)
}

// TestTunnelPortIsInTheChosenRange covers the forward staying out of the way of both privileged
// ports and the range the kernel hands out on its own.
func TestTunnelPortIsInTheChosenRange(t *testing.T) {
	t.Parallel()

	for _, destination := range []string{"a@b", "weave@host", "someone@somewhere.example"} {
		port := engine.Remote{Destination: destination}.TunnelPort("127.0.0.1:8787")
		assert.GreaterOrEqual(t, port, 19000)
		assert.Less(t, port, 20000)
	}
}

// ssmEngine is an engine reached by a transport that is not ssh, written the way a user would
// configure AWS Session Manager, to show weave needs to know nothing about it.
func ssmEngine() engine.Remote {
	return engine.Remote{
		Exec: []string{
			"aws", "ssm", "start-session", "--target", "i-0abc",
			"--document-name", "AWS-StartInteractiveCommand",
			"--parameters", `command=["{{command_json}}"]`,
		},
		Forward: []string{
			"aws", "ssm", "start-session", "--target", "i-0abc",
			"--document-name", "AWS-StartPortForwardingSession",
			"--parameters", `{"portNumber":["{{remote_port}}"],"localPortNumber":["{{local_port}}"]}`,
		},
		User:    "weave",
		Socket:  "weave",
		Session: "weave",
	}
}

// TestATransportIsRemoteWithoutAnSshDestination covers a transport that needs no host, which is
// what lets the engine keep no public address.
func TestATransportIsRemoteWithoutAnSshDestination(t *testing.T) {
	t.Parallel()

	assert.True(t, ssmEngine().IsRemote())
}

// TestAttachArgvUsesTheConfiguredTransport covers weave running what it was given rather than
// anything it knows about a cloud.
func TestAttachArgvUsesTheConfiguredTransport(t *testing.T) {
	t.Parallel()

	argv := ssmEngine().AttachArgv("%9")

	assert.Equal(t, []string{"aws", "ssm", "start-session"}, argv[:3])
	assert.NotContains(t, argv, "ssh")
}

// TestAttachArgvFillsTheCommandPlaceholder covers the transport being told what to run, escaped
// for the json the placeholder sits inside.
func TestAttachArgvFillsTheCommandPlaceholder(t *testing.T) {
	t.Parallel()

	argv := ssmEngine().AttachArgv("%9")
	parameters := argv[len(argv)-1]

	assert.NotContains(t, parameters, "{{command_json}}")
	assert.Contains(t, parameters, "view-9")

	// The script quotes with backslashes, and those have to survive being placed inside json or
	// the far side is handed a different script than the one weave built.
	assert.Contains(t, parameters, `\\`)
}

// TestAttachArgvStepsIntoTheEnginesAccount covers a transport arriving as its own account rather
// than the one that owns the tmux server.
func TestAttachArgvStepsIntoTheEnginesAccount(t *testing.T) {
	t.Parallel()

	assert.Contains(t, ssmEngine().AttachArgv("%9")[len(ssmEngine().AttachArgv("%9"))-1],
		`sudo -u `+"'weave'")
}

// TestNoUserMeansNothingIsSteppedInto covers a transport that already arrives as the right
// account, like an ssh destination naming one.
func TestNoUserMeansNothingIsSteppedInto(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{Destination: "weave@host", Socket: "weave", Session: "weave"}

	assert.NotContains(t, remote.AttachArgv("%9")[3], "sudo")
}

// TestTunnelArgvFillsBothPorts covers the forward being told which two ends to join.
func TestTunnelArgvFillsBothPorts(t *testing.T) {
	t.Parallel()

	argv := ssmEngine().TunnelArgv("127.0.0.1:19123", "127.0.0.1:8787")
	parameters := argv[len(argv)-1]

	assert.Equal(t, []string{"aws", "ssm", "start-session"}, argv[:3])
	assert.Contains(t, parameters, `"portNumber":["8787"]`)
	assert.Contains(t, parameters, `"localPortNumber":["19123"]`)
}

// TestSshIsStillTheShorthand covers the common case staying one line of config.
func TestSshIsStillTheShorthand(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{Destination: "weave@host", Socket: "weave", Session: "weave"}

	assert.Equal(t, "ssh", remote.AttachArgv("%9")[0])
	assert.Equal(t, "ssh", remote.TunnelArgv("127.0.0.1:1", "127.0.0.1:2")[0])
}

// TestStringNamesTheTransport covers an error saying how the engine is reached.
func TestStringNamesTheTransport(t *testing.T) {
	t.Parallel()

	assert.Contains(t, ssmEngine().String(), "aws ssm start-session")
}
