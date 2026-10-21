The hub embeds the agent builds it hands out from this folder. They're made
by `go run ./cmd/agentbundle` (the Dockerfiles and build-image.sh run it) and
aren't kept in git. Without them the hub runs fine, but can't install or
update agents.
