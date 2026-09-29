import hashlib
import json
import sys
import tempfile
import types
import unittest
from pathlib import Path
from unittest.mock import patch

from askbase_eval.data import prepare_t2
from askbase_eval.metrics import read_qrels


class PublicDataTests(unittest.TestCase):
    def test_prepare_keeps_full_corpus_when_limiting_queries(self):
        tables = {
            "corpus": [{"_id": "d1", "title": "标题", "text": "第一段"},
                       {"_id": "d2", "title": "", "text": "第二段"}],
            "queries": [{"_id": "q1", "text": "问题一"}, {"_id": "q2", "text": "问题二"}],
            "default": [{"query-id": "q1", "corpus-id": "d1", "score": 1},
                        {"query-id": "q2", "corpus-id": "d2", "score": 1}],
        }
        datasets = types.SimpleNamespace(load_dataset=lambda _, config, **kwargs: tables[config])
        hub = types.SimpleNamespace(HfApi=lambda: types.SimpleNamespace(dataset_info=lambda _: types.SimpleNamespace(sha="abc123")))
        with tempfile.TemporaryDirectory() as directory, patch.dict(sys.modules, {"datasets": datasets, "huggingface_hub": hub}):
            folder = Path(directory)
            source = prepare_t2(folder, query_limit=1)
            self.assertEqual(source["corpus_count"], 2)
            self.assertEqual(source["query_count"], 1)
            self.assertEqual(len((folder / "corpus.jsonl").read_text(encoding="utf-8").splitlines()), 2)
            self.assertEqual((folder / "qrels.tsv").read_text(encoding="utf-8"), "q1\t0\td1\t1\n")
            fingerprints = []
            for doc_id, name, text in (("d1", "标题", "第一段"), ("d2", "d2", "第二段")):
                content = "public-benchmark:" + doc_id + "\n" + name + "\n" + text
                fingerprints.append(doc_id + "\t" + hashlib.sha256(content.encode()).hexdigest() + "\n")
            payload = "".join(sorted(fingerprints))
            self.assertEqual(source["source_hash"], hashlib.sha256(payload.encode()).hexdigest())
            self.assertEqual(json.loads((folder / "source.json").read_text(encoding="utf-8"))["revision"], "abc123")

    def test_qrels_reject_duplicate_labels(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "qrels.tsv"
            path.write_text("q1\t0\td1\t1\nq1\t0\td1\t1\n", encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "Duplicate"):
                read_qrels(path)


if __name__ == "__main__":
    unittest.main()
