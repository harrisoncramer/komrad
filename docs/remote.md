# Running weave on a remote box

Weave normally runs everything on one machine. The panel you look at, the service behind it, and the agents themselves all share a tmux server on your laptop.

You can also split it. The panel stays on your machine, and everything else moves to a box somewhere else. The agents run there, they keep running when you close your laptop, and the panel connects to them over SSH. This page is how to set that up.

## What the two halves are

The engine is the part that does the work. It is a service called `weave serve`, a Postgres database, a tmux server that owns every agent's terminal, and the agents themselves.

The panel is the part you look at. It draws the sidebar, it handles your keys, and it runs on your own machine so that your clipboard, your editor and your own tmux all still work.

The only thing that crosses between them is a connection. The panel talks to the engine's API over a forwarded port, and when you open an agent, the panel runs a command over SSH that attaches to that agent's terminal on the engine. Your local tmux draws what comes back.

## What you need before starting

An AWS account, and a named profile in your AWS config that can reach it.

An S3 bucket for Terraform to keep its state in. Terraform does not create this for you, because it needs somewhere to record what it created.

A key pair used for nothing else. Make one with `ssh-keygen -t ed25519 -f ~/.ssh/weave_engine`. The public half goes into the machine, the private half stays on your laptop, and neither is shared with anything else you own.

The tools come from mise, so `mise install` gets you Terraform, Packer and the AWS CLI at the versions this repo expects.

## Step one, build the machine image

The image carries everything the box needs before it knows anything about your account. That is the weave binary, the systemd units that run it, and the scripts those units call.

```
mise run packer:build
```

It prints an AMI id when it finishes, which takes around twenty minutes. That id is stamped with the commit it was built from, so you can always trace an image back to the code in it.

## Step two, describe your infrastructure

Copy `infra/terraform/terraform.tfvars.example` to `terraform.tfvars` and fill it in. It wants your account id, your region, your AWS profile, the AMI id you just built, and two things about access.

The first is `ssh_public_key`, which is the public half of the key you made. Terraform writes it into the box, so the key is never baked into the image and never shared between boxes.

The second is `ssh_allowed_cidrs`, which is the list of addresses allowed to reach port 22. Put your own address in it and nothing else. Terraform refuses to accept the whole internet here, on purpose.

Copy `backend.hcl.example` to `backend.hcl` and point it at your state bucket. Both of these files are ignored by git, so your account details never leave your machine.

Then build it.

```
mise run terraform:init
mise run terraform:apply
```

This creates a network, a box, a disk for the agents' work, a Postgres database, and the permissions tying them together. It prints the address of the box when it is done.

## Step three, point the panel at it

An engine is a file in `~/.config/weave/engines/`, named after the engine. It is layered over your own config rather than replacing it, so it holds only what differs from running everything on this machine. Nothing about it is specific to AWS. Weave is told two commands and runs them, so the same shape works for any box you can reach over SSH.

Write `~/.config/weave/engines/remote.toml`.

```toml
[connection]
addr               = "127.0.0.1:8787"
tmux_socket        = "weave-remote"
engine_tmux_socket = "weave"
cache_dir          = "~/.cache/weave-remote"

exec = [
  "ssh", "-t",
  "-o", "ControlMaster=auto",
  "-o", "ControlPersist=10m",
  "-o", "ControlPath=/tmp/cm-weave-%r@%h:%p",
  "-i", "/absolute/path/to/your/weave_engine",
  "weave@ENGINE_ADDRESS",
  "{{command}}",
]

forward = [
  "ssh", "-N",
  "-o", "ControlMaster=auto",
  "-o", "ControlPersist=10m",
  "-o", "ControlPath=/tmp/cm-weave-%r@%h:%p",
  "-i", "/absolute/path/to/your/weave_engine",
  "-L", "{{local_port}}:127.0.0.1:{{remote_port}}",
  "weave@ENGINE_ADDRESS",
]
```

`exec` is how weave runs one command on the engine, and `{{command}}` is where that command goes. `forward` is how weave carries a port to the engine, and the two placeholders are the local and remote ends of it.

The `ControlMaster` settings matter more than they look. They tell SSH to keep the first connection open and run every later command over it. Without them, opening an agent builds a whole new connection each time, which is slow enough to see.

Then bind a key to it, next to the one that opens the local panel, by adding a line to your tmux config.

```
run-shell 'weave tmux'
run-shell 'weave tmux --engine remote'
```

Each line binds the key that engine's config asks for, so give the remote one a `panel_key` of its own and both answer from the same tmux.

Open it with that key rather than by running `weave open` yourself. The key opens the panel as a popup over the session you are already in, which is what keeps its splits and its copy-mode inside the popup. Running `weave open` in a terminal instead attaches the panel as that terminal's only client, and then the panel owns the whole screen: the splits are not respected and copy-mode runs across everything. The popup is the right shape for a remote engine just as it is for a local one, because the panel runs on your machine either way.

Without `--engine` you get the engine on this machine, so the same binary reaches either. A name weave does not know is refused with a list of the ones it does.

The socket and the cache directory are in that file for a reason. They keep a remote panel's tmux server and its log apart from a local one's, so both can run at once and neither reads the other's state.

```
weave --engine remote doctor
```

is the quickest way to see which files were layered and whether the engine answers.

## Keeping it current

Two things go stale, and they are updated differently.

The weave binary changes every time you merge something. Updating it is quick and does not disturb the agents, because the service is restarted in a way that leaves their terminals alone.

```
mise run remote:deploy
```

The machine image changes when the units or the provisioning scripts change, which is rare. Updating it means building a new image and applying it, and that replaces the box. Every agent on it stops. The disk holding their work follows the new box, and so does the address, but anything mid-conversation is gone. Do it deliberately.

## Getting a shell on it

```
mise run remote:connect
mise run remote:connect -- systemctl status weave
```

The address comes from Terraform, so you do not have to remember it. The connection is shared with the panel's, so a shell you open here is nearly free.

The address outlives the box, so a rebuilt engine answers on the same address with a different host key. The task keeps its host keys in a file of its own and drops the stale one when that happens, rather than making you edit the known hosts you use for everything else.

## How the security works

The box has no password login and no keys in its image. The only way in is the key you made, and the only addresses allowed to try are the ones you listed.

The database password is never written into the image or into the instance's startup script. The startup script carries the name of a secret, and the box fetches the value itself using permissions attached to the box rather than a credential stored on it. A timer refetches it, so rotating the password does not need anyone to log in.

The agents run as their own account, which is not the account you would log in as and cannot become root.

AWS Systems Manager is left installed and permitted even though SSH is the way in. It costs nothing, it needs no open port, and it is how you get back in if you change address or break the key.

## What it costs

The box bills while it runs, and so does the database, whether or not anything is using them. There is no schedule that stops them outside working hours yet. Until there is, taking it down when you are finished with it is a single command, and the state in S3 means building it again later is the same command you used the first time.

```
mise run terraform:destroy
```

This removes the box and the database. Read what it plans to remove before agreeing to it.
