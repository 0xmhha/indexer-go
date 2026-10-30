import importlib.util
from pathlib import Path
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))
spec = importlib.util.spec_from_file_location("ci_docs", ROOT / "scripts/check_docs.py")
docs = importlib.util.module_from_spec(spec)
spec.loader.exec_module(docs)


class Documents(unittest.TestCase):
    def test_links_anchors_unicode_duplicates_and_code_examples(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "README.md").write_text('[다음](guide.md#설명-1) [space](<with space.md>)\n```md\n[x](missing.md)\n```\n')
            (root / "guide.md").write_text('# 설명\n# 설명\n')
            (root / "with space.md").write_text('# title\n')
            self.assertEqual(docs.check(root, ["README.md"]), (2, []))

    def test_broken_anchor_and_outside_link_fail(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "README.md").write_text('[a](#missing) [b](no.md) [c](../outside.md)\n')
            _, errors = docs.check(root, ["README.md"])
            self.assertEqual(len(errors), 3)

    def test_symlinked_document_cannot_read_outside_repository(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "repo"
            root.mkdir()
            outside = root.parent / "outside.md"
            outside.write_text("private fixture must not be parsed\n")
            (root / "README.md").symlink_to(outside)
            self.assertEqual(docs.check(root, ["README.md"]), (0, ["README.md: document leaves repository"]))


if __name__ == "__main__":
    unittest.main()
