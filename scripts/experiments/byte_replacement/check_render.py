"""Runnable smoke check for the experiment's real Semantix assembly adapter."""
import json
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parents[3]
content = 'project=demo; command=go test ./...; delete_allowed=false'
result = subprocess.run(
    ['go', 'run', './scripts/experiments/byte_replacement/render'],
    cwd=root, input=json.dumps([content, content]), text=True,
    capture_output=True, check=True,
)
blocks = json.loads(result.stdout)
assert len(blocks) == 2 and blocks[0] == blocks[1]
assert content in blocks[0] and 'origin=user-curated' in blocks[0]
assert blocks[0].startswith('[semantix-reuse]')
assert blocks[0].rstrip().endswith('[/semantix-reuse]')
rejected = subprocess.run(
    ['go', 'run', './scripts/experiments/byte_replacement/render'],
    cwd=root, input=json.dumps(['x' * 8192]), text=True, capture_output=True,
)
assert rejected.returncode != 0, 'Do not silently compare an empty injection arm'
print('Real L2 rendering, determinism, provenance and budget rejection passed.')
