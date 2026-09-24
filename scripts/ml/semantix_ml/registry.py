"""Checkpoint registry (spec §6): versioned model history under
train/checkpoints/vNNNN with a `current` symlink, so publish and rollback
are both one atomic symlink swap and the rerank server just follows
current/ on SIGHUP. I-2(c): every prior version stays on disk.
"""

import json
import re
import shutil
from pathlib import Path

_VERSION_RE = re.compile(r"^v(\d{4})$")


def _checkpoints_dir(train_dir):
    return Path(train_dir) / "checkpoints"


def list_versions(train_dir):
    d = _checkpoints_dir(train_dir)
    if not d.is_dir():
        return []
    out = [p.name for p in d.iterdir() if p.is_dir() and _VERSION_RE.match(p.name)]
    return sorted(out)


def current_version(train_dir):
    link = Path(train_dir) / "current"
    if not link.is_symlink():
        return None
    target = link.resolve().name
    return target if _VERSION_RE.match(target) else None


def _point_current(train_dir, version):
    link = Path(train_dir) / "current"
    tmp = Path(train_dir) / ".current.tmp"
    if tmp.is_symlink() or tmp.exists():
        tmp.unlink()
    tmp.symlink_to(Path("checkpoints") / version)
    tmp.replace(link)  # atomic swap on POSIX


def publish(train_dir, checkpoint_dir, metrics):
    """Move a staged checkpoint into the registry as the next version and
    point `current` at it. Returns the version string."""
    train_dir = Path(train_dir)
    versions = list_versions(train_dir)
    next_n = int(_VERSION_RE.match(versions[-1]).group(1)) + 1 if versions else 1
    version = f"v{next_n:04d}"
    dest = _checkpoints_dir(train_dir) / version
    dest.parent.mkdir(parents=True, exist_ok=True)
    shutil.move(str(checkpoint_dir), str(dest))
    (dest / "metrics.json").write_text(json.dumps(metrics, indent=2) + "\n")
    _point_current(train_dir, version)
    return version


def rollback(train_dir, version=None):
    """Point `current` at an explicit version, or at the one immediately
    before the current version when omitted."""
    versions = list_versions(train_dir)
    if version is None:
        cur = current_version(train_dir)
        if cur is None or cur not in versions:
            raise ValueError("rollback: no current version to roll back from")
        idx = versions.index(cur)
        if idx == 0:
            raise ValueError("rollback: no earlier version exists")
        version = versions[idx - 1]
    elif version not in versions:
        raise ValueError(f"rollback: unknown version {version!r}")
    _point_current(train_dir, version)
    return version
