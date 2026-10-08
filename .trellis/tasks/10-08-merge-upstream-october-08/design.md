# 兼容合并设计

## 集成边界

在 `merge/upstream-20261008` 从 fork 固定起点合入上游固定 SHA，保留 merge 双亲。本会话管理分支、索引和提交，子代理负责明确不重叠的文件范围。验证后将本地 main 快进；若输入有外部变化先审查，不覆盖。

## 迁移契约

| 上游 | fork |
| --- | --- |
| 0025_proxy_catalog | 0028_proxy_catalog |
| 0026_credential_names | 0029_credential_names |
| 0027_remove_request_log_output_timing | 0030_remove_request_log_output_timing |
| 0028_request_log_client_ip | 0031_request_log_client_ip |
| 0029_group_priority | 0032_group_priority |

冻结旧 0001–0027 的身份和 DDL，新增迁移只改文件名、ID、导出符号与调用者，DDL 内部名字保留。特别注意上游清理迁移引用旧 `ValidateRecoverable0024` / `Validate0024`，在 fork 应为 0027；其新增 `ValidateCurrent0024` 映射 `ValidateCurrent0027` 并挂到旧输出计时注册项，使升级后账本校验接受已清理列。不能全仓替换重叠数字。

检查新旧 schema fixture 的 Name/Priority/计时列和硬编码前缀；按语义 ID 查找边界。新增真实 SQLite 27→32 升级回归，保存账本与关键业务数据、凭据身份和备注；二次执行幂等。MySQL/PostgreSQL 路径保留，无外部 DSN 时说明未实测。

## 凭据契约

上游 `name` 与 fork `note` 为独立字段，别名 255 字符且无控制字符，备注保留既有 2048 字符/null 清空语义。更新请求、normalize 返回值、事务写入、registry 名称更新、modern DTO/投影/dirty/保存/三语言均检查组合。别名更新的运行时优化不得丢弃备注或改变可信周期。

## 自动合并审查

检查 gateway/state 的可信额度重置独立于调度冷却；requestlog 使用上游新首响应统计后仍保留模型用量精确边界与缺失用量提示。检查捐献路由、凭据创建、配置字段、CPA/Bifrost 诊断、两套 UI 模型用量入口及 GHCR 文件。接纳上游锁文件，不额外升级依赖。

## 验证与回退

按 Makefile 跑完整门禁，本机缺 make 时逐项执行。Windows 既有日志转义、路径/脚本问题不能直接沿用旧结论；出现失败时在本轮固定起点的仓库外 LF worktree 做相同命令的精确基线比较。仅改变被证明与 Git 内容一致的工作树换行，不修改全局配置。

本轮只对临时测试数据库执行迁移。部署前需备份数据库及配套密钥；升级后的旧二进制回退需配对升级前备份。集成分支保留供审查，不强制重置或清理用户目录。
