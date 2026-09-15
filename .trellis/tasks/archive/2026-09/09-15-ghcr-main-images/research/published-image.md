# GHCR 首次发布验收

2026-09-15，工作提交 `7a522cb7ffaf22a7bc53e166de0ef1ffefa75e13` 已推送到 `0401lucky/gpt-load` 的main。首次 [Main images运行](https://github.com/0401lucky/gpt-load/actions/runs/34913061296) 全部成功。

| 阶段 | 结果 |
| --- | --- |
| build | success，00:24:41–00:31:14 UTC |
| verify linux/amd64 | success，原生ubuntu-24.04，实际拉取/运行镜像 |
| verify linux/arm64 | success，原生ubuntu-24.04-arm，实际拉取/运行镜像 |
| promote | success，00:32:09 UTC完成；读取远端main后更新并复核两个通道 |

## 可用引用

```text
ghcr.io/0401lucky/gpt-load:latest
ghcr.io/0401lucky/gpt-load:main
ghcr.io/0401lucky/gpt-load:sha-7a522cb7ffaf22a7bc53e166de0ef1ffefa75e13
ghcr.io/0401lucky/gpt-load@sha256:0c7153b38654bd7d42dbb6789a01cd5544b772005df9555a3311de69fb512b10
```

此时三个tag均指向上述digest。两个平台的OCI revision均为完整工作提交，version均为 `2.0.0-dev.1.g7a522cb7ffaf`，runtime user均为 `10001:10001`。

独立匿名OCI验证没有使用GitHub用户令牌、Docker登录凭据或其它私有授权：三个tag的manifest、两个平台config及18个layer HEAD全部HTTP200；跨主机的blob重定向未转发Authorization。结果在 [registry-verification.json](registry-verification.json)。GitHub两个原生runner还实际拉取并运行了digest镜像；[github-run.json](github-run.json)保留job/step结论。

本机Packages REST缺少read:packages不妨碍上述匿名registry验证。没有为了验证而扩大个人令牌权限，也未改包的访问设置。main流水线明确不执行漏洞扫描，不能把这里的运行验证称为完整tag Release审计。

## 服务器更新

现有Compose若仍指向 `ghcr.io/tbphp/gpt-load:2`，先把gpt-load服务的image改为 `ghcr.io/0401lucky/gpt-load:latest`；使用本仓库新版Compose时可通过GPT_LOAD_IMAGE固定上述digest或提交tag。

在原部署目录、原Compose项目和原数据卷下执行：

```bash
docker compose pull gpt-load
docker compose up -d --no-deps gpt-load
docker compose ps gpt-load
```

保留原.env、数据库、auth.key和encryption.key，更新前备份数据库与密钥；既有1.x数据仍不能原地迁移到2.x。捐献联动还需配置独立DONATION_INTEGRATION_TOKEN并在new-api填写对应连接。本次只完成镜像发布，没有操作服务器。
