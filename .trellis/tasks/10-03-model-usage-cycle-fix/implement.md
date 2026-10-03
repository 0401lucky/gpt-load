# 执行计划

1. 最终方案批准后 task.py start，使用 Trellis implement/check 子代理。
2. 补普通 60 秒冷却污染周期、旧检查点恢复的失败回归。
3. 修复 health → gateway → state 的额度证据传递，覆盖重复通知、较短可信期限、重启和身份代次。
4. API 透传缺失/部分计数，同步两套投影、UI 和三语言。
5. 检查非流式采集，本地 fixture 验证；无法证明解析缺陷则记录证据限制。
6. 定向 Go 回归后 make check，逐项验收，更新周期来源与兼容规范。
7. 交付本地 diff、验证及部署/回滚说明，不擅自部署。

重点：health/decision.go、execution_judge.go、gateway/handler.go、state/model_cooldown.go、runtime_checkpoint.go、control/group_model_usage.go、requestlog/group_model_usage.go、两套前端矩阵。

规划批准前不改产品代码；保留已有未跟踪目录；测试不连接生产数据库。
