import importlib.util
import json
import tempfile
import unittest
from pathlib import Path

from askbase_eval.metrics import compare, score


class MetricTests(unittest.TestCase):
    @unittest.skipUnless(importlib.util.find_spec("ir_measures"), "ir-measures is not installed")
    def test_ranked_relevant_document_scores_and_writes_run(self):
        with tempfile.TemporaryDirectory() as directory:
            folder = Path(directory)
            (folder / "qrels.tsv").write_text("q1\t0\td1\t1\nq1\t0\td2\t1\n", encoding="utf-8")
            collection = {"generatedAt": "2026-01-01", "datasetId": 1, "mode": "direct",
                          "topK": 10, "minScore": 0.2, "sourceHash": "hash", "embeddingModel": "test",
                          "results": [{"id": "q1", "text": "question", "hits": [
                              {"documentId": "wrong", "rank": 1},
                              {"documentId": "d1", "rank": 2},
                              {"documentId": "d1", "rank": 3}]}]}
            (folder / "collection.json").write_text(json.dumps(collection), encoding="utf-8")
            (folder / "source.json").write_text(json.dumps({"source_hash": "hash", "query_count": 1}), encoding="utf-8")
            report = score(folder / "collection.json", folder / "qrels.tsv", folder / "out", folder / "source.json")
            self.assertAlmostEqual(report["metrics"]["R@10"], 0.5)
            self.assertAlmostEqual(report["metrics"]["RR"], 0.5)
            self.assertEqual(report["queries"]["q1"]["missing"], ["d2"])
            self.assertEqual(len((folder / "out/run.tsv").read_text(encoding="utf-8").splitlines()), 2)

    def test_compare_rejects_different_public_revisions(self):
        with tempfile.TemporaryDirectory() as directory:
            folder = Path(directory)
            base = {"source": {"revision": "old"}, "queries": {"q1": {}}, "metrics": {"R@10": 0.5}}
            current = {**base, "source": {"revision": "new"}}
            (folder / "base.json").write_text(json.dumps(base), encoding="utf-8")
            (folder / "current.json").write_text(json.dumps(current), encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "same public dataset"):
                compare(folder / "current.json", folder / "base.json", folder / "comparison.json")

    @unittest.skipUnless(importlib.util.find_spec("ir_measures"), "ir-measures is not installed")
    def test_empty_results_are_scored_as_misses(self):
        with tempfile.TemporaryDirectory() as directory:
            folder = Path(directory)
            (folder / "qrels.tsv").write_text("q1\t0\td1\t1\n", encoding="utf-8")
            collection = {"generatedAt": "2026-01-01", "datasetId": 1, "mode": "planned",
                          "topK": 10, "minScore": 0.2, "sourceHash": "hash", "embeddingModel": "test",
                          "results": [{"id": "q1", "text": "question", "hits": []}]}
            (folder / "collection.json").write_text(json.dumps(collection), encoding="utf-8")
            (folder / "source.json").write_text(json.dumps({"source_hash": "hash", "query_count": 1}), encoding="utf-8")
            report = score(folder / "collection.json", folder / "qrels.tsv", folder / "out", folder / "source.json")
            self.assertEqual(report["metrics"]["R@10"], 0)
            self.assertEqual(report["metrics"]["Success@5"], 0)

    @unittest.skipUnless(importlib.util.find_spec("ir_measures"), "ir-measures is not installed")
    def test_at_ten_ignores_documents_after_tenth_chunk(self):
        with tempfile.TemporaryDirectory() as directory:
            folder = Path(directory)
            (folder / "qrels.tsv").write_text("q1\t0\trelevant\t1\n", encoding="utf-8")
            hits = [{"documentId": "irrelevant", "rank": rank} for rank in range(1, 11)]
            hits.append({"documentId": "relevant", "rank": 11})
            collection = {"generatedAt": "2026-01-01", "datasetId": 1, "mode": "direct",
                          "topK": 20, "minScore": 0.2, "sourceHash": "hash", "embeddingModel": "test",
                          "results": [{"id": "q1", "text": "question", "hits": hits}]}
            (folder / "collection.json").write_text(json.dumps(collection), encoding="utf-8")
            (folder / "source.json").write_text(json.dumps({"source_hash": "hash", "query_count": 1}), encoding="utf-8")
            report = score(folder / "collection.json", folder / "qrels.tsv", folder / "out", folder / "source.json")
            self.assertEqual(report["metrics"]["R@10"], 0)


if __name__ == "__main__":
    unittest.main()
