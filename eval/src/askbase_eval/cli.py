"""固定版本公开检索基准的命令行工作流。

Python 负责公开数据下载、标准指标计算和报告；Go 负责调用 AskBase 的实际索引与检索实现。
"""

from __future__ import annotations

import argparse
import subprocess
from pathlib import Path

from .data import prepare_t2
from .metrics import compare, score
from .sample import sample_t2

BACKEND = Path(__file__).resolve().parents[3] / "be"


def _go(command: str, *, source: Path, dataset_id: int, output: Path | None = None,
        mode: str = "direct", top_k: int = 10, min_score: float | None = None,
        rebuild: bool = False) -> None:
    args = ["go", "run", "./cmd/retrievaldump", command,
            "--input", str(source.resolve()), "--dataset-id", str(dataset_id)]
    if rebuild:
        args.append("--rebuild")
    if output is not None:
        args += ["--output", str(output.resolve()), "--mode", mode, "--top-k", str(top_k)]
        if min_score is not None:
            args += ["--min-score", str(min_score)]
    subprocess.run(args, cwd=BACKEND, check=True)


def parser() -> argparse.ArgumentParser:
    root = argparse.ArgumentParser(description="Evaluate AskBase retrieval on a public labeled corpus")
    commands = root.add_subparsers(dest="command", required=True)
    prepare = commands.add_parser("prepare", help="download and pin the Chinese T2Retrieval benchmark")
    prepare.add_argument("--data", type=Path, required=True)
    prepare.add_argument("--query-limit", type=int, help="development only; keeps the full corpus")
    sample = commands.add_parser("sample", help="create a reproducible smoke-test subset from local data")
    sample.add_argument("--source", type=Path, required=True)
    sample.add_argument("--output", type=Path, required=True)
    sample.add_argument("--corpus-limit", type=int, default=1000)
    sample.add_argument("--query-limit", type=int, default=20)
    sample.add_argument("--seed", type=int, default=42)
    index = commands.add_parser("index", help="index the entire public corpus into a dedicated AskBase dataset")
    index.add_argument("--data", type=Path, required=True)
    index.add_argument("--dataset-id", type=int, required=True)
    index.add_argument("--rebuild", action="store_true", help="recreate embeddings and chunks after changing index settings")
    run = commands.add_parser("run", help="collect retrieval rankings and score against published qrels")
    run.add_argument("--data", type=Path, required=True)
    run.add_argument("--dataset-id", type=int, required=True)
    run.add_argument("--output", type=Path, required=True)
    run.add_argument("--mode", choices=("direct", "planned"), default="direct")
    run.add_argument("--top-k", type=int, default=10)
    run.add_argument("--min-score", type=float)
    comparison = commands.add_parser("compare", help="compare two reports on the same public corpus")
    comparison.add_argument("--current", type=Path, required=True)
    comparison.add_argument("--baseline", type=Path, required=True)
    comparison.add_argument("--output", type=Path, required=True)
    return root


def main(argv: list[str] | None = None) -> int:
    args = parser().parse_args(argv)
    if args.command == "prepare":
        source = prepare_t2(args.data, args.query_limit)
        print(f"Prepared {source['dataset']}@{source['revision']}: "
              f"{source['corpus_count']} documents, {source['query_count']} queries")
    elif args.command == "sample":
        subset = sample_t2(args.source, args.output, args.corpus_limit, args.query_limit, args.seed)
        print(f"Prepared smoke subset: {subset['corpus_count']} documents, {subset['query_count']} queries")
    elif args.command == "index":
        _go("index", source=args.data / "corpus.jsonl", dataset_id=args.dataset_id, rebuild=args.rebuild)
    elif args.command == "run":
        if args.top_k < 10 or args.top_k > 50:
            raise ValueError("--top-k must be in 10..50 for @10 metrics")
        args.output.mkdir(parents=True, exist_ok=True)
        collected = args.output / "retrieval.json"
        _go("collect", source=args.data / "queries.jsonl", dataset_id=args.dataset_id,
            output=collected, mode=args.mode, top_k=args.top_k, min_score=args.min_score)
        report = score(collected, args.data / "qrels.tsv", args.output, args.data / "source.json")
        print(f"Scored {report['queryCount']} queries: "
              f"R@10={report['metrics']['R@10']:.4f}, nDCG@10={report['metrics']['nDCG@10']:.4f}")
    else:
        result = compare(args.current, args.baseline, args.output)
        print(f"Compared reports: {len(result['recallRegressions'])} recall regressions")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
