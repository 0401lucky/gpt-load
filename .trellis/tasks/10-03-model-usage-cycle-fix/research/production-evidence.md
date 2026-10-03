# 只读生产证据（2026-10-03）

目标：yunyou-99（SSH 43477），gpt-load-2，cline 分组 4；镜像 sha-080a1531ab0d60fb439743ad0afa60b2904e9a6e。仅执行管理 GET、SQLite mode=ro 查询和 docker logs；无生产写入、重启或推理请求。

## 窗口问题

12:49（北京时间）接口 146 行全部与相同窗口的 request_logs 请求数和六项 token 合计一致，无差异。

| 凭据 ID | 页面窗口起点（北京时间） | 恢复时间 | 窗口 token | 近 24h 已记录 token |
|---|---|---|---:|---:|
| 557 | 10-02 23:07:15.138 | 10-03 16:07:28.533 | 0 | 13,915,023 |
| 558 | 10-02 23:07:15.566 | 10-03 16:16:33.224 | 0 | 12,908,589 |
| 567 | 10-02 22:46:36.342 | 10-03 17:28:18.484 | 0 | 14,777,290 |
| 478 | 10-03 00:58:03.954 | 10-03 22:22:39.235 | 0 | 8,876,977 |

docker 日志 UTC 10-02 15:06:15，凭据 557 冷却到 15:07:15.138，status_code=200；该时刻和窗口起点精确一致。邻近请求错误 upstream_rate_limited，summary 为 Vercel upstream 创建 stream 失败。凭据 478 最后一次短冷却 UTC 16:57:03 到 16:58:03.954，status_code=0。次日 UTC 02:00:39 返回 429，冷却到 14:22:39.235，此次将前一个短冷却终点迁移成周期起点。

这些日志说明 HTTP status 200/0 也可能携带限流证据，不能用 status==429 筛选可信周期；必须在健康判定处保留“明确额度重置”的独立来源。

## missing

补充可信重置样例：只读查询 request_logs 的 error_summary，得到 `Error 429: Daily free limit reached on model deepseek/deepseek-v4.1-flash. Try again in 10h 3m`，另有 11h 30m、14h 36m、17h 42m、4h 53m。普通 Try again in 文案本身不足以证明额度周期。调度解析额外加一分钟安全缓冲，不能把缓冲后的期限直接当统计周期重置时刻。

近 24h 成功 complete 10,683 条，成功 missing 318 条；missing 中 DeepSeek 312、mimo 2、space-bunny 4，全部 stream=false。最近样例 HTTP 200、error_code/summary 为空、usage_state=missing；协议 openai-completions，operation=chat_completion。

现有记录证明 token 缺失，不能证明上游未返回还是解析未识别；没有抓取用户请求/响应正文。不能用总成功数或平均 token 推算补账。已存历史 token 与准确额度周期的恢复是两个问题。
