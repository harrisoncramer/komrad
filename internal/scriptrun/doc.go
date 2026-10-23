// Package scriptrun runs the scripts hung off an agent's life and records each run: the
// provisioner that makes its directory, the deprovisioner that removes it, and the workflow's
// hooks at each moment. Hooks queued under the same agent or task run one moment at a time, in
// order.
package scriptrun
