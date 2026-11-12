# aws-ssm-pf

[![ci](https://github.com/moveeeax/aws-ssm-pf/actions/workflows/ci.yml/badge.svg)](https://github.com/moveeeax/aws-ssm-pf/actions/workflows/ci.yml)
[![go](https://img.shields.io/badge/go-1.26%2B-00ADD8)](https://go.dev)
[![license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

> `ssm-pf` — forward a port to any instance without touching the console.

A thin, friendly wrapper around `aws ssm start-session` with the
`AWS-StartPortForwardingSession` document. It finds your instance with a fuzzy
picker instead of copy-pasted instance IDs, remembers what you connected to,
and gets out of the way when a local port is already taken.

```bash
ssm-pf --remote-port 5432 --local-port 15432
# ? Select an instance:
#    1) db-primary (i-0abc…)   Online  Amazon Linux
#    2) db-replica (i-0def…)   Online  Amazon Linux
# Forwarding localhost:15432 -> db-primary (i-0abc…):5432 (Ctrl-C to stop)
```

The same works for a database that only a bastion can reach:

```bash
ssm-pf --host orders.cluster-abc.eu-central-1.rds.amazonaws.com:5432 -l 15432 -q bastion
# Forwarding localhost:15432 -> orders.cluster-abc.eu-central-1.rds.amazonaws.com:5432 via bastion (i-0abc…) (Ctrl-C to stop)
```

## Why

`aws ssm start-session --target i-0123… --document-name AWS-StartPortForwardingSession
--parameters '{"portNumber":["5432"],"localPortNumber":["15432"]}'` is a mouthful,
and it assumes you already know the instance id. `ssm-pf` keeps the exact same
underlying mechanism — no new daemon, no stored credentials — but adds the
ergonomics:

- **Fuzzy finder** over SSM-managed instances by `Name` tag or id.
- **Only online targets**: filtered to `PingStatus == Online`, so dead agents
  never show up.
- **Remote hosts**: `--host` forwards through the instance to an RDS,
  ElastiCache or OpenSearch endpoint (or any host the instance can reach) with
  `AWS-StartPortForwardingSessionToRemoteHost`; `--rds` picks the endpoint from
  your RDS instances and clusters.
- **Remembered tunnels**: recent targets, remote hosts and port mappings are
  saved; `--recent` reconnects the last one.
- **Local-port auto-pick**: if your preferred local port is busy, it grabs a free
  one and tells you. The listener is loopback-only — a tunnel into a private VPC
  is never exposed to the rest of your network.
- **SSO / assume-role aware**: `--profile` / `--region` (and the usual env vars)
  flow straight into the default credential chain.
- **No leaked tunnels**: the `aws` CLI is only a launcher — the real tunnel is
  held by the `session-manager-plugin` it forks. `ssm-pf` runs that whole tree in
  its own process group and tears the group down on Ctrl-C, on `SIGTERM`, and on
  its own exit, so a live session into your VPC can't outlive the command.

## Install

```bash
go install github.com/moveeeax/aws-ssm-pf@latest
```

`go install` names the binary after the last element of the module path, so
this puts `aws-ssm-pf` (not `ssm-pf`) into `$(go env GOBIN)`, or
`$(go env GOPATH)/bin` when `GOBIN` is unset. Either call it as `aws-ssm-pf`
or give it the short name used in the examples below:

```bash
alias ssm-pf=aws-ssm-pf
```

From a clone, `make build` produces `./ssm-pf` and `make install` puts `ssm-pf`
into the same directory `go install` would use.

### Prerequisites

- [AWS CLI v2](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)
- [session-manager-plugin](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html)
- An IAM identity allowed to `ssm:StartSession` on the target and to
  `ssm:DescribeInstanceInformation` (plus `ec2:DescribeInstances` for `Name` tags).
  `--host` and `--rds` run the `AWS-StartPortForwardingSessionToRemoteHost`
  document, so `ssm:StartSession` must allow that document too; `--rds` also
  needs `rds:DescribeDBInstances` and `rds:DescribeDBClusters`.

## Usage

```
ssm-pf [flags]
ssm-pf list [flags]
```

| Flag | Description |
|------|-------------|
| `-r`, `--remote-port` | Remote port on the instance, or on `--host` (required unless `--host` has a port or `--rds` is set) |
| `-l`, `--local-port` | Local port to bind (auto-picked if `0` or busy) |
| `-t`, `--target` | Instance id (`i-…` / `mi-…`), skipping the picker |
| `-q`, `--query` | Fuzzy filter for the instance picker |
| `--host` | Forward through the instance to this host: a DNS name or IP, optionally `host:port` (`[ipv6]:port` for IPv6) |
| `--rds` | Pick the remote host and port from RDS instance and cluster endpoints |
| `--rds-query` | Fuzzy filter for the RDS endpoint picker |
| `--recent` | Reuse the most recent target, host and ports (any flag you also pass wins) |
| `--no-input` | Never prompt; require an unambiguous target |
| `--document-name` | SSM document (default `AWS-StartPortForwardingSession`, or `AWS-StartPortForwardingSessionToRemoteHost` with `--host`/`--rds`) |
| `--profile` | AWS shared-config profile |
| `--region` | AWS region |
| `--aws-binary` | Path to the `aws` CLI (default: `aws` in `PATH`) |

With `--host` the remote port is a port on that host, and the parameters sent
to SSM are `{"host":[…],"portNumber":[…],"localPortNumber":[…]}`. A port in
`--host` and a different `--remote-port` is an error. With `--recent`, an
explicit `--host` replaces the remembered host and its port, so pass
`--remote-port` (or `host:port`) along with it.

`--rds` lists the writer and reader endpoint of every DB cluster and the
endpoint of every DB instance, then asks you to pick one (filtered by
`--rds-query`, and unambiguous under `--no-input`). The endpoint's port is the
remote port unless `--remote-port` is set. The instance picker runs after it to
choose the instance that forwards the traffic. `--rds` cannot be combined with
`--host` or `--recent`.

See [`examples/`](examples/) for more, including how to watch the whole flow
without AWS using `--aws-binary /usr/bin/true`.

## How it works

1. `ssm.DescribeInstanceInformation` lists managed instances; anything not
   `Online` is dropped.
2. `ec2.DescribeInstances` resolves the `Name` tag for `i-*` instances (best
   effort — on-prem `mi-*` activations keep their SSM-reported name).
3. The fuzzy picker ranks candidates (contiguous and word-boundary matches win);
   a unique match connects immediately.
4. With `--rds`, `rds.DescribeDBClusters` and `rds.DescribeDBInstances` list
   endpoints for a second fuzzy picker.
5. A free local port is chosen, the tunnel is recorded to
   `${XDG_CONFIG_HOME:-~/.config}/ssm-pf/history.json` (mode `0600`), and control
   is handed to `aws ssm start-session`.

Nothing is ever passed through a shell: the `aws` CLI is executed directly with
an argument vector. On top of that, every value that ends up in that vector —
target, remote host, document name, profile, region, ports — is validated
first, so a value
shaped like another CLI option (say `--endpoint-url=…`) is rejected up front
rather than reinterpreted by the CLI. Validation runs *before* AWS is contacted,
a port is bound, or anything is written to the history file.

## Development

```bash
make build   # go build -o ssm-pf .
make install # build ssm-pf into GOBIN (or GOPATH/bin)
make test    # go test -race ./...
make lint    # gofmt -l . && go vet ./...
```

The AWS surface sits behind the `Lister` and `EndpointLister` interfaces, so
the pickers, port selection, history, and session-argument construction are
all unit-tested without touching AWS.

## License

MIT — see [LICENSE](LICENSE).
