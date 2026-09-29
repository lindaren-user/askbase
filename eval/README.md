# AskBase 公开检索评测

当前只评估公开测试集；私有文档黄金集是后续 TODO。默认数据集为 [mteb/T2Retrieval](https://huggingface.co/datasets/mteb/T2Retrieval)，使用发布方提供的中文语料、查询及人工相关性标签。评测程序不会自动生成或人工重标标签。

## 为什么评测侧使用 Python

公开数据集下载、TREC 格式处理和检索指标计算在 Python 中有现成的工具；这里直接使用 `datasets`、`huggingface-hub` 和 `ir-measures`，减少自写指标造成的口径偏差。产品的索引、嵌入和检索仍由 Go 代码执行，Python 只负责准备数据、编排运行、计分及生成报告。

## 运行

准备 Python 环境（建议 Python 3.10–3.13），安装依赖：

```powershell
python -m pip install -e './eval[download]'
```

下载完整语料及标注。数据集的 Hugging Face commit SHA 会写入 `source.json`，以后重跑时使用本地快照：

```powershell
askbase-retrieval-eval prepare --data ./eval/data/t2
```

### 先用小样本试跑

已有完整快照时，可在本地生成可重复的试跑子集；`--corpus-limit` 控制语料条数（默认 1000），`--query-limit` 控制问题数（默认 20），`--seed` 控制随机补充语料的结果（默认 42）：

```powershell
askbase-retrieval-eval sample --source ./eval/data/t2 --output ./eval/data/t2-smoke --corpus-limit 1000 --query-limit 20
```

子集保留所选问题的全部已标注文档，再随机补足语料。为试跑子集创建**单独的空知识库**，将下方 `index` 和 `run` 命令的 `--data` 改为 `./eval/data/t2-smoke`。改变语料数量、问题数量或随机种子后，应生成新子集并使用新的空知识库。由于候选语料减少，试跑分数可能偏高，只用于验证流程和估算耗时，不能与完整公开基准直接比较。

在 AskBase 中创建一个**空的专用知识库**，记下 ID，并保证 PostgreSQL、嵌入模型服务及 `be/config.yaml` 可用。然后执行：

```powershell
askbase-retrieval-eval index --data ./eval/data/t2 --dataset-id 123
askbase-retrieval-eval run --data ./eval/data/t2 --dataset-id 123 --output ./eval/results/t2-direct
```

`index` 可在中断后重跑；每条公开 passage 作为一个文档索引，长 passage 使用通用文本切分器切开，保留原始文档 ID。指标按原始文档去重和计分。当前基准只覆盖纯文本 passage 的通用切分、嵌入、向量/全文召回及融合排序；不经过正式文件解析流程，也不覆盖书籍、论文、简历、问答对策略。T2Retrieval 完整语料约 11.9 万条、查询约 2.3 万条；索引会调用嵌入服务，完整运行的耗时和费用取决于所用模型。请先确认专用数据库与模型额度。

TODO：为五种分块策略分别选择匹配的公开原始文档，复用正式解析与分块流水线，并在运行时校验知识库策略与基准类型一致。当前知识库策略不会改变评测导入器的切分方式，不能据此比较策略优劣。

修改通用切分参数或嵌入实现后使用 `index --rebuild` 重建已有公开语料；只改查询检索参数时直接重跑 `run` 即可。

开发时也可用 `prepare --query-limit 20` 仅限制**问题数**；它仍保留完整语料，不会减少索引成本。`prepare` 会固定下载版本，不修改 AskBase 业务 API。

查询规划可单独测：

```powershell
askbase-retrieval-eval run --data ./eval/data/t2 --dataset-id 123 --mode planned --output ./eval/results/t2-planned
```

`planned` 对每个查询调用路由模型，完整测试集会产生额外模型调用。

比较两个相同数据集版本的报告：

```powershell
askbase-retrieval-eval compare --baseline ./eval/results/t2-direct/report.json --current ./eval/results/t2-planned/report.json --output ./eval/results/comparison.json
```

## 指标与产物

- `Success@5`：前五个结果是否至少有一个相关文档。
- `R@5`、`R@10`：已知相关文档中进入前五／前十的比例。
- `RR`：在只保留前十结果的 run 上计算，因此等于 MRR@10 的逐题平均。
- `nDCG@10`：考虑相关性等级及排名的标准指标。

指标使用 [ir-measures](https://ir-measur.es/en/latest/getting-started.html)计算，`run.tsv` 保存标准 TREC run。原始采集结果存于 `retrieval.json`，`report.json`/`report.md` 包含总体与逐题结果。RRF 融合后按实际排名计分，原始向量/全文分数只作诊断，不用于重排。评测前会验证索引语料的 ID 和正文指纹与公开数据快照一致；不一致时拒绝计分。

公开基准的相关性标签可能并未穷尽所有相关文档，因此“未标注”不能自动解释为“不相关”。目前不评估无答案拒答、私有知识库、最终答案或引用质量。

TODO：引入带答案位置或块级相关性标签的公开测试集，增加块级指标。当前一个 passage 被切成多个 chunk 后，只要命中其中任意 chunk，就按原始 passage 的相关性计分；这无法证明该 chunk 包含答案。
