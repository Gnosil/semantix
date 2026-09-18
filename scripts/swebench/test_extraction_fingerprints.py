import json
from pathlib import Path
from types import SimpleNamespace
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch


from run_bench import mirror_fingerprint_paths


class ExtractionFingerprints(unittest.TestCase):
    def test_context_path_keys_are_in_dependency_coverage(self):
        for key in ('path', 'paths', 'file', 'files', 'file_path', 'file_paths',
                    'source_path', 'destination_path', 'directory', 'directories',
                    'dir', 'dirs', 'cwd', 'workdir', 'filepath', 'filename'):
            with self.subTest(key=key), tempfile.TemporaryDirectory() as td:
                root = Path(td)
                (root / 'README.md').write_text('unchanged')
                (root / 'cache.py').write_text('original cache')
                args = {'nested': {'path': 'README.md'}, key: ['cache.py']}
                line = {'role': 'assistant', 'tool_calls': [{'name': 'edit_file', 'arguments': args}]}
                mirror = root / 'mirror.jsonl'
                mirror.write_text((json.dumps(line) + '\n') * 2)
                self.assertEqual(mirror_fingerprint_paths(mirror, root), ['README.md', 'cache.py'])

    def test_incomplete_or_unsafe_paths_do_not_grant_partial_freshness(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td) / 'workspace'
            root.mkdir()
            (root / 'README.md').write_text('unchanged')
            (root / 'pkg').mkdir()
            (root / 'pkg/cache.py').write_text('original cache')
            (Path(td) / 'outside.py').write_text('outside')
            (root / 'alias.py').symlink_to(root / 'pkg/cache.py')
            (root / 'alias-dir').symlink_to(root / 'pkg', target_is_directory=True)
            (root / 'escape-dir').symlink_to(Path(td), target_is_directory=True)
            mirror = root / 'mirror.jsonl'
            for path in ('deleted.py', 'pkg', '../outside.py', 'pkg/../README.md',
                         str(Path(td) / 'outside.py'), 'alias.py',
                         'alias-dir/cache.py', 'escape-dir/outside.py'):
                with self.subTest(path=path):
                    mirror.write_text(json.dumps({'role': 'assistant', 'tool_calls': [
                        {'name': 'read_file', 'arguments': {'paths': ['README.md', path]}}
                    ]}) + '\n')
                    self.assertEqual(mirror_fingerprint_paths(mirror, root), [],
                                     'partial fingerprints must not certify a whole extracted slice')

    def test_regular_paths_nested_json_and_no_observed_paths(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            (root / 'cache.py').write_text('unchanged')
            mirror = root / 'mirror.jsonl'
            for argument in ({'paths': ['cache.py', str(root / 'cache.py')]},
                             json.dumps({'nested': {'file_path': 'cache.py'}})):
                mirror.write_text(json.dumps({'tool_calls': [{'arguments': argument}]}) + '\n')
                self.assertEqual(mirror_fingerprint_paths(mirror, root), ['cache.py'])
            mirror.write_text(json.dumps({'tool_calls': [{'arguments': {'command': 'python -m pytest'}}]}) + '\n')
            self.assertEqual(mirror_fingerprint_paths(mirror, root), [])

    def test_fingerprint_cli_path_ambiguity_is_not_certified(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            (root / 'cache.py').write_text('different file')
            mirror = root / 'mirror.jsonl'
            for path in (' cache.py', 'cache.py ', 'cache,other.py'):
                with self.subTest(path=path):
                    (root / path).write_text('observed file')
                    mirror.write_text(json.dumps({'tool_calls': [{'arguments': {'path': path}}]}) + '\n')
                    self.assertEqual(mirror_fingerprint_paths(mirror, root), [])


if __name__ == '__main__':
    unittest.main()
