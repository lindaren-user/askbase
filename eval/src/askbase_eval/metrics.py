"""使用标准 TREC 指标评估原始文档的检索排名。

评测侧使用 Python，便于直接使用公开数据集工具和 ir-measures 的标准指标；
真实索引与召回仍由 Go 服务执行，避免在评测侧重写产品检索逻辑。
"""

from __future__ import annotations

import json
from collections import defaultdict
from pathlib import Path


def read_qrels(path: Path) -> dict[str, dict[str, int]]:
    labels: dict[str, dict[str, int]] = defaultdict(dict)
    for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        fields = line.split()
        if len(fields) != 4 or fields[1] != "0":
            raise ValueError(f"Invalid qrels line {number}")
        query_id, _, doc_id, raw_relevance = fields
        try:
            relevance = int(raw_relevance)
        except ValueError as exc:
            raise ValueError(f"Invalid relevance on line {number}") from exc
        if relevance < 0 or doc_id in labels[query_id]:
            raise ValueError(f"Duplicate document or negative relevance on line {number}")
        labels[query_id][doc_id] = relevance
    if not labels:
        raise ValueError("qrels are empty")
    if any(not any(value > 0 for value in docs.values()) for docs in labels.values()):
        raise ValueError("Every evaluated query needs a positive qrel")
    return dict(labels)


def score(collection_path: Path, qrels_path: Path, output_dir: Path, source_path: Path | None = None) -> dict:
    try:
        import ir_measures
    except ImportError as exc:
        raise RuntimeError("Install the evaluator: pip install -e ./eval") from exc

    collection = json.loads(collection_path.read_text(encoding="utf-8"))
    source = json.loads(source_path.read_text(encoding="utf-8")) if source_path else None
    if source and collection.get("sourceHash") != source.get("source_hash"):
        raise ValueError("Indexed corpus does not match this public benchmark revision")
    if collection.get("topK", 0) < 10:
        raise ValueError("Collection must request at least 10 hits for @10 metrics")
    labels = read_qrels(qrels_path)
    results = collection.get("results")
    if not isinstance(results, list) or not results:
        raise ValueError("Collection has no query results")
    if source and source.get("query_count") != len(results):
        raise ValueError("Collection query count does not match this public benchmark snapshot")
    ids = [item["id"] for item in results]
    if len(ids) != len(set(ids)) or set(ids) != set(labels):
        raise ValueError("Collection query IDs must exactly match qrels query IDs")

    qrels = [ir_measures.Qrel(qid, doc_id, relevance)
             for qid, docs in labels.items() for doc_id, relevance in docs.items()]
    run = []
    per_query = {}
    run_lines = []
    for item in results:
        qid = item["id"]
        seen = set()
        retrieved = []
        # TODO: 公开 qrels 只标注原始 passage；需要带证据位置或块级标签的公开数据集，
        # 才能判断命中的具体 chunk 是否包含答案，并单独报告块级检索质量。
        for hit in item["hits"][:10]:
            doc_id = str(hit["documentId"])
            if doc_id in seen:
                continue
            seen.add(doc_id)
            retrieved.append(doc_id)
        for rank, doc_id in enumerate(retrieved, 1):
            score_value = float(11 - rank)  # 保留融合后的排名；不同召回路的原始分数不可直接比较。
            run.append(ir_measures.ScoredDoc(qid, doc_id, score_value))
            run_lines.append(f"{qid} Q0 {doc_id} {rank} {score_value:g} askbase")
        relevant = {doc_id for doc_id, rel in labels[qid].items() if rel > 0}
        per_query[qid] = {"query": item["text"], "retrieved": retrieved,
                          "missing": sorted(relevant - set(retrieved)),
                          "durationMs": item.get("durationMs", 0),
                          "routeError": item.get("routeError", "")}

    measures = [ir_measures.Success@5, ir_measures.R@5, ir_measures.R@10,
                ir_measures.RR, ir_measures.nDCG@10]
    aggregate = ir_measures.calc_aggregate(measures, qrels, run)
    for metric in ir_measures.iter_calc(measures, qrels, run):
        per_query[metric.query_id][str(metric.measure)] = metric.value
    summary = {str(measure): aggregate[measure] for measure in measures}
    summary["meanDurationMs"] = sum(item["durationMs"] for item in per_query.values()) / len(per_query)
    report = {"source": source,
              "collection": {key: collection[key] for key in ("generatedAt", "datasetId", "mode", "topK", "minScore", "sourceHash", "documentCount", "chunkTargetRunes", "chunkOverlapRunes", "embeddingModel", "routerModel") if key in collection},
              "queryCount": len(per_query), "metrics": summary, "queries": per_query}
    output_dir.mkdir(parents=True, exist_ok=True)
    (output_dir / "run.tsv").write_text("\n".join(run_lines) + "\n", encoding="utf-8")
    (output_dir / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    (output_dir / "report.md").write_text(render_report(report), encoding="utf-8")
    return report


def render_report(report: dict) -> str:
    lines = ["# AskBase 公开检索测评", "", f"问题数：{report['queryCount']}", "", "## 总体指标", ""]
    if report.get("source") and report["source"].get("subset", {}).get("kind") == "smoke":
        lines[0] = "# AskBase 公开检索试跑"
        lines[2:2] = ["数据范围：缩小后的试跑子集，不能作为完整公开基准分数。", ""]
    for name, value in report["metrics"].items():
        lines.append(f"- {name}: {value:.4f}")
    lines.extend(["", "## 漏召回最多的问题（前 30）", ""])
    failures = sorted(report["queries"].items(), key=lambda pair: (-len(pair[1]["missing"]), pair[0]))
    for qid, item in failures[:30]:
        if not item["missing"]:
            break
        lines.extend([f"### {qid}", "", item["query"], "",
                      f"- 未进入 Top 10：{', '.join(item['missing'][:20])}",
                      f"- Top 10：{', '.join(item['retrieved'])}", ""])
    return "\n".join(lines) + "\n"


def compare(current_path: Path, baseline_path: Path, output_path: Path) -> dict:
    current = json.loads(current_path.read_text(encoding="utf-8"))
    baseline = json.loads(baseline_path.read_text(encoding="utf-8"))
    if current["source"] != baseline["source"] or set(current["queries"]) != set(baseline["queries"]):
        raise ValueError("Baseline and current report must use the same public dataset revision and queries")
    changes = {name: current["metrics"][name] - value for name, value in baseline["metrics"].items()
               if name in current["metrics"]}
    regressions = []
    for qid, item in current["queries"].items():
        old = baseline["queries"][qid]
        delta = item.get("R@10", 0) - old.get("R@10", 0)
        if delta < 0:
            regressions.append({"id": qid, "recallDelta": delta,
                                "newMissing": sorted(set(item["missing"]) - set(old["missing"]))})
    result = {"metricDeltas": changes, "recallRegressions": sorted(regressions, key=lambda row: row["recallDelta"])}
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return result
