# 捐献文案多语言核对

2026-09-14。范围仅为 new-api 的 `web/src/i18n/locales/{en,zh,zh-TW,fr,ru,ja,vi}.json`，未改组件、测试、static-keys.ts 或任务进度。

## 结果

- 七个语言文件各新增 **156** 条，共 **1,092** 条。每个文件从 6,762 条增至 6,918 条，原有 6,762 条译文及 `{translation: {...}}` 包装全部保持不变。
- 初始缺失项为 153 条；与实现方确认后额外补充 `Duplicate key`、`Instance ID`。完整前端复核又补入 `Please shorten the campaign description.`，配合按后端 UTF-8 字节边界校验说明长度；当前 [源键清单](i18n-keys.json) 有 **182** 个键，全部七语齐全。原 4000 字符提示已从当前调用清单替换，但字典中的旧译文保留。
- 共享 `Duplicate` 在法语、俄语、日语、越南语中原为“复制/克隆”动作译文。实现方已把捐献重复状态改为 `Duplicate key`；本次保留共享译文，新增专用状态翻译。
- 英文使用原文；其余六语均完成翻译，没有把新增英文句子作为占位值。永久额度、单次提交上限、部分成功、原行位置恢复、账号暂停与暂存到期的含义保持区分，没有引入领奖上限、到期或追回规则。

简体中文 R18 逐字匹配：

> 请勿使用主账号的 API key。捐献后的使用可能因平台风控或其他原因导致账号受限、封禁，或 key 失效，请确认能承担相关风险。

## 验证

使用 Python 以显式 UTF-8 读取原始快照、最新源键清单和七个 JSON，逐文件验证：

| 检查 | 结果 |
| --- | --- |
| 缺失键 / 空值 / 非字符串值 | 0 |
| JSON 重复键 | 0 |
| `{{quota}}` / `{{line}}` 等占位符集合不一致 | 0 |
| 既有译文变更 / 删除 / 包装或元数据变更 | 0 |
| 非英文语言的新增值等于英文源键 | 0 |
| UTF-8 解码错误、BOM、替换字符及常见编码损坏标记 | 0 |
| 源键 `Enter a 32–256 character integration credential.` | 保留准确的 U+2013 en dash，七语键名完全一致 |
| R18 简体中文 | exact match |

原始语义快照存于本地临时文件 `C:/Users/lucky0401/AppData/Local/Temp/donation-i18n-baseline-4x0aw7b6/baseline.json`，不进入提交。新增块通过 JSON 转义后定向追加，没有重排或重写旧内容。

格式检查（new-api/web 工作目录）已通过：

```powershell
& '.\node_modules\.bin\oxfmt.exe' --check 'src/i18n/locales/en.json' 'src/i18n/locales/zh.json' 'src/i18n/locales/zh-TW.json' 'src/i18n/locales/fr.json' 'src/i18n/locales/ru.json' 'src/i18n/locales/ja.json' 'src/i18n/locales/vi.json'
```

`git diff --check -- web/src/i18n/locales` 无输出。没有运行 `i18n:sync`、全局 `format:check` 或格式写入；前端类型检查、交互及浏览器验证由主会话的完整前端验收继续执行。
