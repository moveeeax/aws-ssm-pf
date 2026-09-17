# examples

Common `ssm-pf` invocations.

Forward a Postgres port, picking the instance interactively:

```bash
ssm-pf --remote-port 5432 --local-port 15432
```

Skip the picker with a fuzzy query (connects when it uniquely matches):

```bash
ssm-pf -r 6379 -q cache-prod --no-input
```

Target a specific instance id and let the local port auto-pick if busy:

```bash
ssm-pf --target i-0123456789abcdef0 --remote-port 5432
```

Reuse the last tunnel:

```bash
ssm-pf --recent
```

Use an SSO / assume-role profile and an explicit region:

```bash
ssm-pf --profile prod-admin --region eu-central-1 -r 5432
```

List reachable targets without connecting:

```bash
ssm-pf list --profile prod-admin
```

## Try it without AWS

`--aws-binary` swaps the underlying CLI, so you can watch the flow end to end
against a stub:

```bash
ssm-pf --target i-0123456789abcdef0 --remote-port 5432 --local-port 15432 --aws-binary /usr/bin/true
cat "${XDG_CONFIG_HOME:-$HOME/.config}/ssm-pf/history.json"   # the tunnel was remembered
```
