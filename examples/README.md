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

Reach an RDS endpoint through a bastion (the port can go in `--host` or in
`--remote-port`):

```bash
ssm-pf --host orders.cluster-abc.eu-central-1.rds.amazonaws.com:5432 -l 15432 -q bastion
ssm-pf --host 10.0.12.34 -r 6379 -t i-0123456789abcdef0
```

Pick the database from your RDS instances and clusters instead of typing the
endpoint, then pick the bastion:

```bash
ssm-pf --rds --rds-query orders -q bastion -l 15432
```

Reuse the last tunnel (a remembered remote host comes back too):

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
ssm-pf --target i-0123456789abcdef0 --host db.internal.example:5432 --aws-binary /usr/bin/true
```
