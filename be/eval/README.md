# AskBase 路由与检索评测

评测器读取本地私有黄金集，并调用 Chat 使用的同一个 `QueryPlanner` 和
`SearchQueries` 链路。它只评估路由、查询计划和证据检索，不评价最终回答。

## 准备黄金集

复制 `cases.example.jsonl` 为 `cases.jsonl`，再换成真实知识库中的问题和证据。
`cases.jsonl` 与 `results/` 已被 Git 忽略，避免私人内容被提交。

每行是一条独立 JSON：

```json
{"id":"alpha-launch-time","datasetId":1,"history":[],"question":"项目 Alpha 什么时候上线？","expectedRoute":"direct","relevantEvidence":[{"documentName":"项目Alpha说明书.pdf","contains":["2024年6月","正式上线"]}]}
```

- `expectedRoute` 必须是 `none`、`direct`、`contextual_rewrite` 或 `decomposition`。
- `documentName` 必须与知识库中的文档名一致。
- `contains` 中的文本必须全部出现在同一个召回 Chunk 内；匹配忽略大小写。
- 知识库无答案但仍应检索的题目使用 `"expectNoEvidence": true`，不要填写
  `relevantEvidence`。
- `none` 用例不会访问知识库，因此可以省略 `datasetId`。

## 运行

在 `be` 目录执行：

```powershell
go run ./cmd/rageval --cases ./eval/cases.jsonl --output ./eval/results
```

使用 `--runs 3` 等重复次数检查 LLM 路由漂移。`--top-k` 和 `--min-score`
未指定时使用服务配置。命令会连接现有 PostgreSQL、LLM 和嵌入服务，但不会启动
HTTP 服务。

输出包括 `report.json` 和 `report.md`。报告包含总体与分类指标、混淆矩阵、耗时、
路由稳定性、失败原因、查询计划、召回 Chunk 和不含密钥的配置快照。报告自身可能
包含私人文档片段，请勿提交或分享。
