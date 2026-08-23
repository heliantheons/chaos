# Kubernetes deployment contract

This directory defines Chaos as a Kubernetes component without embedding
environment credentials or a promoted release version.

- `base/` owns the workload, Service, and dedicated ServiceAccount.
- `config/` owns non-sensitive defaults and the production Secret schema.
- `ingress/` owns the Chaos `/api` route.

Production release state and SOPS-encrypted values belong to the private
`heliantheon/applications` repository. CI may update only its `overlay/`.

