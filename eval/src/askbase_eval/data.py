"""下载固定版本的公开基准，并保留原始文档 ID。"""

from __future__ import annotations

import json
import hashlib
from pathlib import Path

DATASET = "mteb/T2Retrieval"
SPLIT = "dev"


def prepare_t2(destination: Path, query_limit: int | None = None) -> dict:
    try:
        from datasets import load_dataset
        from huggingface_hub import HfApi
    except ImportError as exc:
        raise RuntimeError("Install download dependencies: pip install -e '.[download]'") from exc

    if query_limit is not None and query_limit < 1:
        raise ValueError("query_limit must be positive")
    destination.mkdir(parents=True, exist_ok=True)
    revision = HfApi().dataset_info(DATASET).sha
    if not revision:
        raise RuntimeError("Could not resolve public dataset revision")
    corpus = load_dataset(DATASET, "corpus", split=SPLIT, revision=revision)
    queries = load_dataset(DATASET, "queries", split=SPLIT, revision=revision)
    qrels = load_dataset(DATASET, "default", split=SPLIT, revision=revision)

    corpus_ids: set[str] = set()
    corpus_fingerprints: list[str] = []
    with (destination / "corpus.jsonl").open("w", encoding="utf-8") as output:
        for row in corpus:
            doc_id = str(row["_id"])
            text = str(row["text"]).strip()
            if not doc_id or doc_id != doc_id.strip() or not text or doc_id in corpus_ids:
                raise ValueError(f"Invalid or duplicate corpus ID: {doc_id!r}")
            corpus_ids.add(doc_id)
            title = str(row.get("title") or "").strip()[:200] or doc_id
            fingerprint = hashlib.sha256(f"public-benchmark:{doc_id}\n{title}\n{text}".encode()).hexdigest()
            corpus_fingerprints.append(f"{doc_id}\t{fingerprint}\n")
            output.write(json.dumps({"id": doc_id, "title": title, "text": text}, ensure_ascii=False) + "\n")

    query_ids: set[str] = set()
    with (destination / "queries.jsonl").open("w", encoding="utf-8") as output:
        for row in queries:
            if query_limit is not None and len(query_ids) >= query_limit:
                break
            query_id = str(row["_id"])
            text = str(row["text"]).strip()
            if not query_id or query_id != query_id.strip() or not text or query_id in query_ids:
                raise ValueError(f"Invalid or duplicate query ID: {query_id!r}")
            query_ids.add(query_id)
            output.write(json.dumps({"id": query_id, "text": text}, ensure_ascii=False) + "\n")

    labeled_queries: set[str] = set()
    with (destination / "qrels.tsv").open("w", encoding="utf-8", newline="") as output:
        for row in qrels:
            query_id, doc_id = str(row["query-id"]), str(row["corpus-id"])
            if query_id not in query_ids:
                continue
            if doc_id not in corpus_ids:
                raise ValueError(f"qrels document {doc_id!r} is missing from corpus")
            relevance = int(row["score"])
            if relevance < 0:
                raise ValueError("Negative relevance label")
            output.write(f"{query_id}\t0\t{doc_id}\t{relevance}\n")
            if relevance > 0:
                labeled_queries.add(query_id)
    missing = query_ids - labeled_queries
    if missing:
        raise ValueError(f"Queries without positive labels: {sorted(missing)[:5]}")
    corpus_hash = hashlib.sha256("".join(sorted(corpus_fingerprints)).encode()).hexdigest()
    source = {"dataset": DATASET, "revision": revision, "split": SPLIT,
              "corpus_count": len(corpus_ids), "query_count": len(query_ids),
              "query_limit": query_limit, "source_hash": corpus_hash}
    (destination / "source.json").write_text(json.dumps(source, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return source
