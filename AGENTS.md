# Chaos

This repository owns the Helios delivery and object-storage service.

## Boundaries

- Reusable mail client primitives belong to `heliantheon/common`; templates and delivery policy stay here.
- Reusable authentication guards belong to `heliantheon/aegis-go`.
- Keep storage-provider details behind the service layer.

## Commands

```bash
make test
make lint
make build
make run
```

## Verification

- Run all Go tests after handler or provider changes.
- Do not log SMTP, storage, or service-key credentials.
- Preserve object key and template compatibility unless a migration is documented.
