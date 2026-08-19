<p align="center">
  <img src="./assets/brand/hero-ice.png" width="256" alt="Chaos emblem suspended in a clear ice block" />
</p>

<h1 align="center">Chaos</h1>

<p align="center">
  <strong>Delivery and object-storage infrastructure for Helios.</strong><br />
  Helios 的消息投递与对象存储服务。
</p>

## Overview / 项目简介

Chaos centralizes email templates and delivery together with S3-compatible object-storage operations. Its APIs are protected by Aegis, while provider-specific details stay behind the service layer.

Chaos 集中处理邮件模板、消息投递和兼容 S3 的对象存储操作，通过 Aegis 保护 API，并将具体供应商实现隔离在服务层之后。

## Run locally

Chaos needs PostgreSQL and an Aegis service key. SMTP and R2-compatible storage credentials are required for the matching features.

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
