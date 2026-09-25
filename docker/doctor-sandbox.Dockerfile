# The machine komrad doctor is written for: one that has just installed the binary and has
# none of what it depends on. Each stage adds one dependency, so a check can be watched
# failing and then passing without anything else on the machine changing.
#
# The binary is mounted rather than copied, so rebuilding it does not rebuild a stage.

# Nothing but the binary. No tmux, no Postgres, no config file, and an empty home.
FROM debian:bookworm-slim AS bare
RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates procps \
	&& rm -rf /var/lib/apt/lists/*
ENV HOME=/root
WORKDIR /root
ENTRYPOINT ["/usr/local/bin/komrad"]
CMD ["doctor"]

# A tmux too old for the panel. Bullseye ships 3.1c, which has display-popup but not the
# -T flag the binding passes it, so this is the version floor failing for real rather than
# against a stubbed version string.
FROM debian:bullseye-slim AS tmux-old
RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates procps tmux \
	&& rm -rf /var/lib/apt/lists/*
ENV HOME=/root
WORKDIR /root
ENTRYPOINT ["/usr/local/bin/komrad"]
CMD ["doctor"]

# A tmux new enough. Bookworm ships 3.3a, which is exactly the floor, so this stage also
# proves the comparison is not off by one.
FROM bare AS tmux
RUN apt-get update \
	&& apt-get install -y --no-install-recommends tmux \
	&& rm -rf /var/lib/apt/lists/*
