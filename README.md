<p align="center">
  <img src="./assets/brand/hero-ice.png" width="256" alt="Chaos logo" />
</p>

<h1 align="center">Chaos</h1>

Chaos 管的是 Helios 的投递和对象存储：邮件模板、邮件发送、S3 兼容的对象存储操作，都在这一处收敛。接口由 Aegis 保护，各家供应商的细节被挡在服务层后面，调用方看到的是统一的抽象。

Chaos centralizes Helios' delivery and object storage — email templates, sending, and S3-compatible file operations. APIs are protected by Aegis, and provider-specific details stay behind the service layer.

## 本地运行

需要 PostgreSQL，以及一个 Aegis 服务密钥。SMTP 和 R2 兼容存储的凭据只有用到对应功能时才需要。

```bash
cp example.toml config.toml
make run
```

数据库 schema 在 [`sql/`](sql/) 目录下。

## 开发

```bash
make test
make lint
make build
```