-- Chaos 数据库 Schema（运维/模板/邮件）
-- PostgreSQL 18 语法
-- 业务表由 GORM AutoMigrate 在服务启动时创建，这里只负责建库。

SELECT 'CREATE DATABASE chaos'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'chaos')\gexec