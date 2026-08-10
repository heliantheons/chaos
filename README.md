# Chaos

Chaos provides shared delivery infrastructure for Helios: email templates and sending, plus S3-compatible object storage operations. Its API is protected by Aegis.

## Run locally

Chaos needs MySQL and an Aegis service key. SMTP and R2-compatible storage credentials are required for the matching features.

```bash
cp example.toml config.toml
make run
```

The database schema is under [`sql/`](sql/).

## Development

```bash
make test
make lint
make build
```
