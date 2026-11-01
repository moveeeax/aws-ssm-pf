# Changelog

All notable changes to this project are listed here. Versions follow
[Semantic Versioning](https://semver.org/).

## Unreleased

The published `v0.1.0` tag (and the `v0` tag next to it) points at commit
`58f532c`, which is not part of the current `main` history. `go install
github.com/moveeeax/aws-ssm-pf@latest` therefore still installs that commit,
without any of the fixes below, until a new tag is cut from `main`.

### Security

- Every value passed to the `aws` CLI (target, document name, profile, region,
  ports) is validated before AWS is contacted, a port is bound or history is
  written. A `--target` such as `--endpoint-url=http://…` is rejected instead
  of being read by the CLI as an extra option.
- The `aws` CLI and the `session-manager-plugin` it forks run in their own
  process group, which is torn down on Ctrl-C, on `SIGTERM` and on exit, so a
  tunnel no longer outlives the command.

### Fixed

- `--recent` no longer replaces a `--target` given on the command line, and
  it now also reuses the remembered local port unless `--local-port` is set.
- A missing, unreadable or corrupt history file prints a warning and starts
  fresh instead of aborting the port forward.
- The history file is written through a temp file and rename, so an
  interrupted write cannot leave truncated JSON behind.
- An out-of-range `--local-port` is an error instead of being replaced with a
  random free port.
- `make install` builds the binary as `ssm-pf`; the README explains that
  `go install` names it `aws-ssm-pf`.

### Changed

- Go 1.26 or newer is required (`go` directive raised from 1.22 in v0.1.0).
- AWS SDK for Go v2 modules updated (`aws-sdk-go-v2` v1.47.5, `config`
  v1.33.13, `service/ec2` v1.343.0, `service/ssm` v1.81.2).

## v0.1.0

First release: fuzzy instance picker over SSM-managed instances, remembered
tunnels with `--recent`, local-port auto-pick and the `list` subcommand.
