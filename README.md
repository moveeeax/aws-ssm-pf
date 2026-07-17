# aws-ssm-pf

> `ssm-pf` — forward a port to any instance without touching the console.

**Status:** 🚧 In development

## Overview

CLI wrapper for AWS SSM port-forwarding with fuzzy instance selection.

## Features

- Fuzzy finder over instances by name tag / id
- Remembers recent targets and port mappings
- Wraps `aws ssm start-session` port-forward document
- Local port auto-pick if busy
- Works with SSO/assume-role profiles

## Stack

Go + aws-sdk-go-v2 + interactive fuzzy picker.

## Usage

```bash
ssm-pf --remote-port 5432 --local-port 15432
```

## License

MIT
