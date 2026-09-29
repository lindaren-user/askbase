import hashlib
import json
import tempfile
import unittest
from pathlib import Path

from askbase_eval.cli import parser
from askbase_eval.metrics import render_report
from askbase_eval.sample import sample_t2


class SampleTests(unittest.TestCase):
    def test_sample_preserves_labels_and_matches_index_fingerprint(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "full"
            source.mkdir()
            rows = [{"id": f"d{i}", "title": f"标题{i}", "text": f"正文{i}"} for i in range(6)]
            (source / "corpus.jsonl").write_text(
                "".join(json.dumps(row, ensure_ascii=False) + "\n" for row in rows), encoding="utf-8")
            (source / "queries.jsonl").write_text(
                "".join(json.dumps({"id": f"q{i}", "text": f"问题{i}"}) + "\n" for i in range(3)),
                encoding="utf-8")
            (source / "qrels.tsv").write_text(
                "q0\t0\td0\t1\nq0\t0\td1\t1\nq1\t0\td2\t1\nq2\t0\td3\t1\n",
                encoding="utf-8")
            (source / "source.json").write_text(json.dumps({
                "dataset": "mteb/T2Retrieval", "revision": "test", "split": "dev",
                "corpus_count": 6, "query_count": 3, "query_limit": None, "source_hash": "full-hash",
            }), encoding="utf-8")

            first = root / "first"
            second = root / "second"
            sample = sample_t2(source, first, corpus_limit=4, query_limit=2, seed=7)
            sample_t2(source, second, corpus_limit=4, query_limit=2, seed=7)
            self.assertEqual((first / "corpus.jsonl").read_bytes(), (second / "corpus.jsonl").read_bytes())
            chosen = [json.loads(line) for line in (first / "corpus.jsonl").read_text(encoding="utf-8").splitlines()]
            self.assertEqual(len(chosen), 4)
            self.assertTrue({"d0", "d1", "d2"}.issubset({row["id"] for row in chosen}))
            self.assertEqual(len((first / "queries.jsonl").read_text(encoding="utf-8").splitlines()), 2)
            self.assertEqual(len((first / "qrels.tsv").read_text(encoding="utf-8").splitlines()), 3)
            fingerprints = []
            for row in chosen:
                content = f"public-benchmark:{row['id']}\n{row['title']}\n{row['text']}"
                fingerprints.append(f"{row['id']}\t{hashlib.sha256(content.encode()).hexdigest()}\n")
            expected_hash = hashlib.sha256("".join(sorted(fingerprints)).encode()).hexdigest()
            self.assertEqual(sample["source_hash"], expected_hash)
            self.assertEqual(sample["subset"]["kind"], "smoke")

            with self.assertRaisesRegex(ValueError, "more labeled documents"):
                sample_t2(source, root / "too-small", corpus_limit=2, query_limit=2)

    def test_cli_exposes_limits_and_report_marks_smoke_run(self):
        args = parser().parse_args(["sample", "--source", "full", "--output", "small",
                                    "--corpus-limit", "500", "--query-limit", "10"])
        self.assertEqual((args.corpus_limit, args.query_limit, args.seed), (500, 10, 42))
        report = {"source": {"subset": {"kind": "smoke"}}, "queryCount": 1,
                  "metrics": {}, "queries": {}}
        self.assertIn("不能作为完整公开基准分数", render_report(report))


if __name__ == "__main__":
    unittest.main()
