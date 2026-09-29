"""从本地完整公开数据快照生成可重复的检索试跑子集。"""

from __future__ import annotations

import hashlib
import json
import random
from pathlib import Path

from .metrics import read_qrels


def sample_t2(source_dir: Path, destination: Path, corpus_limit: int = 1000,
              query_limit: int = 20, seed: int = 42) -> dict:
    """保留所选问题的全部标注文档，再随机补足语料数量。"""
    if corpus_limit < 1 or query_limit < 1:
        raise ValueError("corpus_limit and query_limit must be positive")
    if source_dir.resolve() == destination.resolve():
        raise ValueError("Sample output must differ from the source directory")

    source = json.loads((source_dir / "source.json").read_text(encoding="utf-8"))
    if source.get("subset") or source.get("query_limit") is not None:
        raise ValueError("Sample source must be the complete public benchmark snapshot")
    queries = []
    with (source_dir / "queries.jsonl").open(encoding="utf-8") as input_file:
        for line in input_file:
            if len(queries) == query_limit:
                break
            queries.append(json.loads(line))
    if len(queries) != query_limit:
        raise ValueError("Source has fewer queries than query_limit")
    query_ids = [str(row["id"]) for row in queries]
    if len(set(query_ids)) != len(query_ids):
        raise ValueError("Source contains duplicate query IDs")

    labels = read_qrels(source_dir / "qrels.tsv")
    if any(query_id not in labels for query_id in query_ids):
        raise ValueError("Selected query has no relevance labels")
    required_ids = {doc_id for query_id in query_ids for doc_id in labels[query_id]}
    if len(required_ids) > corpus_limit:
        raise ValueError("Selected queries need more labeled documents than corpus_limit")

    corpus_ids = []
    with (source_dir / "corpus.jsonl").open(encoding="utf-8") as input_file:
        for line in input_file:
            corpus_ids.append(str(json.loads(line)["id"]))
    if len(corpus_ids) != source.get("corpus_count") or len(set(corpus_ids)) != len(corpus_ids):
        raise ValueError("Source corpus does not match its manifest")
    if not required_ids.issubset(corpus_ids):
        raise ValueError("Labeled document is missing from source corpus")
    if corpus_limit > len(corpus_ids):
        raise ValueError("Source has fewer documents than corpus_limit")
    candidates = [doc_id for doc_id in corpus_ids if doc_id not in required_ids]
    selected_ids = required_ids | set(random.Random(seed).sample(candidates, corpus_limit - len(required_ids)))

    destination.mkdir(parents=True, exist_ok=True)
    fingerprints = []
    with (source_dir / "corpus.jsonl").open(encoding="utf-8") as input_file, \
            (destination / "corpus.jsonl").open("w", encoding="utf-8") as output_file:
        for line in input_file:
            row = json.loads(line)
            doc_id = str(row["id"])
            if doc_id not in selected_ids:
                continue
            output_file.write(line)
            name = str(row.get("title") or "").strip()[:200] or doc_id
            content = str(row["text"]).strip()
            digest = hashlib.sha256(f"public-benchmark:{doc_id}\n{name}\n{content}".encode()).hexdigest()
            fingerprints.append(f"{doc_id}\t{digest}\n")
    with (destination / "queries.jsonl").open("w", encoding="utf-8") as output_file:
        for row in queries:
            output_file.write(json.dumps(row, ensure_ascii=False) + "\n")
    with (destination / "qrels.tsv").open("w", encoding="utf-8", newline="") as output_file:
        for query_id in query_ids:
            for doc_id, relevance in labels[query_id].items():
                output_file.write(f"{query_id}\t0\t{doc_id}\t{relevance}\n")

    corpus_hash = hashlib.sha256("".join(sorted(fingerprints)).encode()).hexdigest()
    sample = {"dataset": source["dataset"], "revision": source["revision"], "split": source["split"],
              "corpus_count": corpus_limit, "query_count": query_limit, "query_limit": query_limit,
              "source_hash": corpus_hash,
              "subset": {"kind": "smoke", "seed": seed, "parent_source_hash": source["source_hash"]}}
    (destination / "source.json").write_text(json.dumps(sample, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return sample
